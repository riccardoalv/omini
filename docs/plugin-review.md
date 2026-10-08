# How plugins are reviewed

The store lists plugins from an **index** maintained in this repository:
[`internal/plugins/catalog.json`](../internal/plugins/catalog.json). Every Omini
fetches it once a day (`OMINI_PLUGIN_INDEX`, `off` to disable) and falls back to
the copy shipped in its binary when offline. Changing the index is a pull request:
a plugin enters the store, or changes trust level, when a maintainer merges it.

## Trust levels

| Level | Meaning | What it takes |
|---|---|---|
| `unverified` | Not reviewed | Any plugin installed from a GitHub address. Never in the index |
| `experimental` | Partially tested | Code review against the checklist below. Plugins built from a vendor's documentation without hardware stay here |
| `stable` | Tested, known issues documented | Experimental, plus run by the reviewer or by at least two users on real devices for two weeks, and a "Known issues" section (linked from the index entry, `known_issues`) |
| `plug-and-play` | Works out of the box | Stable, plus tested on every model family it claims, a connection test that explains every failure, and no open bug labelled `plugin-bug` |

The publisher badge is separate: `official` plugins are maintained in this
project's account, `community` ones by their authors.

## Requesting a review

Open a **Plugin review** issue with the repository, the release to review, the
level asked for, what it was tested on and its known issues. The reviewer:

1. **Reads the code**: read-only (no request changes configuration; POST only
   for logins or reads that need it, documented), secrets only in `secret`
   fields and never logged, timeouts on every request, optional endpoints that
   fail never fail the collection, no telemetry or calls to third parties.
2. **Checks the repository**: `plugin.yaml` complete, README with the
   least-privilege account, CI with lint and tests whose fixtures come from
   real (anonymized) answers, a license compatible with MIT.
3. **Runs it** (stable and above) against the devices listed, or collects
   reports from users who did.
4. **Opens the pull request** that adds or updates the index entry: `trust`,
   `reviewed_version` (the release reviewed), `reviewed_at` and `known_issues`.

A trust level belongs to a release. When users install a newer release, the
store says it was not reviewed yet (it keeps the level, as a hint); the author
asks for a new review by opening another issue. A plugin that breaks the rules
(writes to a device, leaks a secret) is removed from the index at once.
