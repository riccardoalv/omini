package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

const maxOutput = 32 << 20

// Integration exposes a plugin as an integration type.
func (m *Manager) Integration(p *Plugin) integration.Integration {
	return &pluginIntegration{m: m, p: p}
}

type pluginIntegration struct {
	m *Manager
	p *Plugin
}

func (i *pluginIntegration) Info() integration.Info {
	man := i.p.Manifest
	return integration.Info{
		Type: man.ID, Name: man.Name, Description: model.Deref(man.Description),
		Kind: integration.KindPlugin, Fields: man.Fields,
	}
}

// Timeout lets the collector give the plugin its own time limit.
func (i *pluginIntegration) Timeout() time.Duration { return timeout(i.p.Manifest) + 5*time.Second }

func (i *pluginIntegration) Collect(ctx context.Context, cfg integration.Config) ([]model.Device, error) {
	resp, err := i.m.run(ctx, i.p, model.PluginActionCollect, cfg)
	if err != nil {
		return nil, err
	}
	return resp.Devices, nil
}

func (i *pluginIntegration) Test(ctx context.Context, cfg integration.Config) (string, error) {
	resp, err := i.m.run(ctx, i.p, model.PluginActionTest, cfg)
	if err != nil {
		return "", err
	}
	return model.Deref(resp.Message), nil
}

// run executes the plugin once: request on stdin, response on stdout.
func (m *Manager) run(ctx context.Context, p *Plugin, action model.PluginAction, cfg integration.Config) (model.PluginResponse, error) {
	var resp model.PluginResponse
	interpreter := m.Interpreter
	if interpreter == "" {
		if err := m.ensureEnv(ctx, p); err != nil {
			return resp, err
		}
		interpreter = p.python()
	}
	state := filepath.Join(p.home, "state", strconv.FormatInt(integration.Instance(ctx), 10))
	if err := os.MkdirAll(state, 0o700); err != nil {
		return resp, err
	}
	req, err := json.Marshal(model.PluginRequest{
		Protocol: p.Manifest.Protocol, Action: action, Config: model.PluginRequestConfig(cfg), StateDir: &state,
	})
	if err != nil {
		return resp, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout(p.Manifest))
	defer cancel()
	cmd := exec.CommandContext(ctx, interpreter, filepath.Join(p.root, p.Manifest.Entrypoint))
	cmd.Dir = p.root
	cmd.Env = append(childEnv(), "PYTHONUNBUFFERED=1", "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdin = bytes.NewReader(req)
	var stdout limitedBuffer
	stdout.max = maxOutput
	var stderr limitedBuffer
	stderr.max = 64 << 10
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 2 * time.Second

	start := time.Now()
	runErr := cmd.Run()
	log := slog.With("plugin", p.Manifest.ID, "action", action, "duration", time.Since(start).Round(time.Millisecond))
	if s := stderr.String(); s != "" {
		log.Debug("plugin log", "stderr", tail(s, 4000))
	}
	if ctx.Err() != nil {
		return resp, fmt.Errorf("plugin %s did not answer within %s", p.Manifest.ID, timeout(p.Manifest))
	}
	if stdout.overflow {
		return resp, fmt.Errorf("plugin %s answered more than %d MB", p.Manifest.ID, maxOutput>>20)
	}
	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		if runErr != nil {
			return resp, fmt.Errorf("plugin %s failed: %w: %s", p.Manifest.ID, runErr, tail(stderr.String(), 500))
		}
		return resp, errNoOutput
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return resp, fmt.Errorf("plugin %s gave an invalid answer: %w", p.Manifest.ID, err)
	}
	if resp.Error != nil {
		return resp, errors.New(*resp.Error)
	}
	if runErr != nil {
		return resp, fmt.Errorf("plugin %s failed: %w", p.Manifest.ID, runErr)
	}
	return resp, nil
}

// limitedBuffer keeps at most max bytes and remembers it overflowed.
type limitedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); len(p) > room {
		b.overflow = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

var _ io.Writer = (*limitedBuffer)(nil)
