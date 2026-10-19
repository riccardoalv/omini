# Changelog

## [1.9.0](https://github.com/riccardoalv/omini/compare/v1.8.1...v1.9.0) (2026-10-19)


### Features

* **appicons:** recognize the app a container image runs ([c28b3fa](https://github.com/riccardoalv/omini/commit/c28b3faf2eb5327c384e34da0d2c1054bbe2c831))
* **collector:** a container is the app the scan found on its port ([230fd8f](https://github.com/riccardoalv/omini/commit/230fd8fb276996c5f05e060bc5ab42da66ecd4b6))
* **plugins:** check and apply every plugin's latest release ([8805f8f](https://github.com/riccardoalv/omini/commit/8805f8fe07ef64fe45d6331fca1351c5bf72fb04))
* **schema:** a device can be a container and the ports it publishes ([b0a0f1e](https://github.com/riccardoalv/omini/commit/b0a0f1e36386ac520321b032a4fa8635627f3b69))
* **web:** an app's panel shows the container that serves it ([fd54e88](https://github.com/riccardoalv/omini/commit/fd54e88a1e8c358635b9410df03213cc8c8ad9b2))
* **web:** update every plugin from the Integrations screen ([9aacd70](https://github.com/riccardoalv/omini/commit/9aacd706eebfb625859aac0cc5fae19cdc542273))


### Bug Fixes

* **collector:** a container app links only to a page the scan saw ([74ab522](https://github.com/riccardoalv/omini/commit/74ab522a5bf92191bfb0a5752d413a575d3404b5))
* **web:** give the device note room for its text ([9f767d4](https://github.com/riccardoalv/omini/commit/9f767d41006c15a959b06b510bd75c11b81aa3f1))

## [1.8.1](https://github.com/riccardoalv/omini/compare/v1.8.0...v1.8.1) (2026-10-19)


### Bug Fixes

* **collector:** a VM or container its host reports is no new device ([c126d36](https://github.com/riccardoalv/omini/commit/c126d3632db1ce11814ddf823a67cd31c8da3fdd))
* **topology:** one machine two integrations report shows both reports ([4ed2506](https://github.com/riccardoalv/omini/commit/4ed2506fe29c1c0b68847301865f77e63600d44d))

## [1.8.0](https://github.com/riccardoalv/omini/compare/v1.7.0...v1.8.0) (2026-10-19)


### Features

* **plugins:** Docker, TrueNAS and Synology DSM in the store ([48eead5](https://github.com/riccardoalv/omini/commit/48eead552d647b170369f0adc8575617834521bf))

## [1.7.0](https://github.com/riccardoalv/omini/compare/v1.6.2...v1.7.0) (2026-10-19)


### Features

* a free note per device ([6a1d2c7](https://github.com/riccardoalv/omini/commit/6a1d2c7ba08b0e48e30171a720c31caed2b5d75e))
* **api:** list people over MCP ([a45d836](https://github.com/riccardoalv/omini/commit/a45d8364a56e68fc3a6a8fcf6671f33354ed1252))
* **api:** manage API tokens; a Bearer token opens GET routes only ([4a5767a](https://github.com/riccardoalv/omini/commit/4a5767a45ad925ecc6d29e70e732899b135e2a88))
* **api:** people, device owners and pausing a person ([9ca3bd8](https://github.com/riccardoalv/omini/commit/9ca3bd82866c1235f22d30db2a770948f5731fb1))
* **api:** read-only MCP server at /mcp ([d6027de](https://github.com/riccardoalv/omini/commit/d6027dee3628ab00581d40d8d3f235d9551d4912))
* **auth:** read-only API tokens, stored by their hash ([5e9d041](https://github.com/riccardoalv/omini/commit/5e9d0419c955829606570b8280bd1edc570d1f66))
* **collector:** carry each device's owner onto the map ([c2cb029](https://github.com/riccardoalv/omini/commit/c2cb02934109909e040ad5f33fa7715dcd136a84))
* **collector:** keep AAAA records and name devices by IPv6 ([e29555b](https://github.com/riccardoalv/omini/commit/e29555becda5341187595dceb2eb59d0d68c3e80))
* **collector:** plan the access points' 2.4 GHz channels ([bada5d6](https://github.com/riccardoalv/omini/commit/bada5d6bdc1ba0965a8e055ca72254cd2e16daad))
* **collector:** resolve a Wi-Fi channel conflict one access point at a time ([b6fc048](https://github.com/riccardoalv/omini/commit/b6fc04840d2a7874b42f9431e74e805b15b42e6d))
* **collector:** test a DNS name Omini set in the DNS checks ([e0a8661](https://github.com/riccardoalv/omini/commit/e0a86612719b9d39e0d27eb470719ca8de1d6974))
* **diagnostics:** keep people's names out of the bundle ([16dc9aa](https://github.com/riccardoalv/omini/commit/16dc9aaa576ee53df62e6fef91d804911a2b03a2))
* **discovery:** read the UPnP gateway's port mappings ([07236ef](https://github.com/riccardoalv/omini/commit/07236ef2162476a2f3aa76639b938d63f32ec7f0))
* **insights:** count DHCP ranges of routers that report no pools ([c056152](https://github.com/riccardoalv/omini/commit/c056152a72e9359e570616978efdddcbf5b46aa1))
* **insights:** duplicate_ip for global IPv6 addresses ([bfa5747](https://github.com/riccardoalv/omini/commit/bfa57479ebfa7c57e6c76e9648e9e4c02c2b1242))
* **insights:** tell what the routers expose to the internet ([76a701a](https://github.com/riccardoalv/omini/commit/76a701af95db4cc8bc721028698b326c4bea659c))
* **insights:** tell when a device asks for a restart ([8da67ae](https://github.com/riccardoalv/omini/commit/8da67aea6dd3fac99566aa6335bb29c8dad1e6d3))
* **insights:** tell when a device uses much of its swap ([c9c6f1c](https://github.com/riccardoalv/omini/commit/c9c6f1c578ff1818f0daf9a349c2a9ea1d6005dd))
* **insights:** tell when a disk or board sensor runs hot ([702a832](https://github.com/riccardoalv/omini/commit/702a83283de9fba7c9a54315ee67763e362e61d2))
* **insights:** tell when a VPN peer that was connected goes down ([1c59918](https://github.com/riccardoalv/omini/commit/1c59918975feb9d9320c578836e654c2a6208d03))
* **insights:** tell when a Wi-Fi network has no password ([e2c6f4a](https://github.com/riccardoalv/omini/commit/e2c6f4a7ee66bf561f446e348464fc40bd1ddb5b))
* **insights:** tell when an enabled service is not running ([13cc09d](https://github.com/riccardoalv/omini/commit/13cc09d974c2f5b6263f1553c1585f6a2240ef8b))
* **insights:** tell when IoT devices share a network with servers ([c371628](https://github.com/riccardoalv/omini/commit/c371628f81abb4694089408b4ce63dbf4dbaf715))
* **insights:** tell when the access points' Wi-Fi channels overlap ([10809f8](https://github.com/riccardoalv/omini/commit/10809f8c9c3e674fe74a26b39dffee8c8a279f08))
* **schema:** Wi-Fi radios, port forwards and the wifi.channel change ([bd39476](https://github.com/riccardoalv/omini/commit/bd39476a230a313897de4f638ca44e92b1fe4863))
* **sdk:** a client can say which methods are safe to send again ([bf6295d](https://github.com/riccardoalv/omini/commit/bf6295d794520ac5cf39b2f6100abc70e727adbd))
* **sdk:** export the radio and port forward models ([70edeff](https://github.com/riccardoalv/omini/commit/70edeff7addc5bf95285ab3d92ef1e9707bc902f))
* **sdk:** shared plugin toolkit (HTTP client, optional reads, apply/verify/undo) ([78724e3](https://github.com/riccardoalv/omini/commit/78724e3325de1e4d5a3699f6d70ed3d793587d2c))
* **snmp:** CDP neighbors from CISCO-CDP-MIB ([0fefb6a](https://github.com/riccardoalv/omini/commit/0fefb6a1e3b41d80c82b3db5c1edf774cbda5147))
* **snmp:** chassis identity and transceivers from ENTITY-MIB ([c1750bd](https://github.com/riccardoalv/omini/commit/c1750bd3a9aa8ba083a5baf05b063f746692d320))
* **snmp:** count the processors from hrProcessorLoad ([4855712](https://github.com/riccardoalv/omini/commit/48557129bff7aadaaee779ea63e52f2b31a1f0f0))
* **snmp:** give each interface its addresses with their prefix ([cc99878](https://github.com/riccardoalv/omini/commit/cc9987876ecbf3a8d6347dc1b053d5ae9c52ee1c))
* **snmp:** give VLAN interfaces their parent port and VLAN id ([612ade5](https://github.com/riccardoalv/omini/commit/612ade5ebba8424564d751a930f1539c87b280eb))
* **snmp:** IPv6 management addresses from LLDP ([c082a22](https://github.com/riccardoalv/omini/commit/c082a2250e32a1f4cbaddb9a309492f7208c78bf))
* **snmp:** read disks from hrStorageFixedDisk ([dbf0eac](https://github.com/riccardoalv/omini/commit/dbf0eacc6d5cdb291659451cdc9c1f268366aa8a))
* **snmp:** read each port's duplex from EtherLike-MIB ([2d109ec](https://github.com/riccardoalv/omini/commit/2d109ecbd00704fec6ee21a6b7ab74a5a4ec7632))
* **snmp:** read IPv6 neighbors from ipNetToPhysicalTable ([e597689](https://github.com/riccardoalv/omini/commit/e59768918cf627e3c58b1c644728f4c91640f59b))
* **snmp:** report total and used RAM in bytes ([dfbbdaa](https://github.com/riccardoalv/omini/commit/dfbbdaa2084c9dc6630765dba90760fc8c3c6cfd))
* **snmp:** spanning tree port roles ([ad5109e](https://github.com/riccardoalv/omini/commit/ad5109efc287b4f2f95bd695d3ffebc70b68cfad))
* **snmp:** temperatures from ENTITY-SENSOR-MIB ([efb12f7](https://github.com/riccardoalv/omini/commit/efb12f76dae8a9f3d0a741f9d841564399cd7ba7))
* **store:** keep a port's optical power and temperature in its history ([15c047b](https://github.com/riccardoalv/omini/commit/15c047bfdf247ba1cb87c9c9093443de9f14e7b6))
* **store:** people and the owner of each device ([8317fff](https://github.com/riccardoalv/omini/commit/8317fffdb5831207aea7d8783346dbea6e93bfcf))
* **topology:** DHCPv6 leases give addresses and names ([7cf541e](https://github.com/riccardoalv/omini/commit/7cf541e4f70763f40ce8459d48c738c258ac8d06))
* **topology:** IPv6 gateways and LANs on internet uplinks ([47cd4b3](https://github.com/riccardoalv/omini/commit/47cd4b314158013a61fef29943716f2a80f8dacf))
* **topology:** name clients by DNS records and DHCP reservations ([bb1d666](https://github.com/riccardoalv/omini/commit/bb1d666ce34d95cd27a5d8a1cd79adee98e13a9f))
* **topology:** NDP ties IPv6 addresses to nodes like ARP ([203b786](https://github.com/riccardoalv/omini/commit/203b78660790f8ea6de40015fdb5927e5ca6889f))
* **web:** a People tab, a device's owner and each person's internet ([8b76410](https://github.com/riccardoalv/omini/commit/8b76410694e71b4f6e92133e75f9654d3ac50d2e))
* **web:** draw a VPN tunnel that is down in red ([18008ca](https://github.com/riccardoalv/omini/commit/18008ca8fe1769c66c4feb5f92a623afabeb4e7e))
* **web:** list what a router exposes to the internet in its panel ([18c5817](https://github.com/riccardoalv/omini/commit/18c581796dd536d6e6009e42ea0c8475305565ce))
* **web:** mark paused devices on the Internet screen ([1503bcf](https://github.com/riccardoalv/omini/commit/1503bcf06e00b275c73bb8cd62ff746d9974310b))
* **web:** place Wi-Fi clients and routed networks in the VLAN filter ([7a93d9b](https://github.com/riccardoalv/omini/commit/7a93d9b5337aabca53f8eb868b5303d73e0fb393))
* **web:** resolve a Wi-Fi channel conflict from its alert ([3d75cf8](https://github.com/riccardoalv/omini/commit/3d75cf8aa7f7938d2ceb601cf7593f3507055336))
* **web:** Settings → API tokens, with the MCP address and a client config ([d813820](https://github.com/riccardoalv/omini/commit/d813820feaef9983d7ce6578109e5645d3045bfc))
* **web:** show an access point's Wi-Fi radios in its panel ([e6955f3](https://github.com/riccardoalv/omini/commit/e6955f393e30354fb0f6fec7ccda02a0690a59ca))
* **web:** show and search devices' IPv6 addresses ([e9c6632](https://github.com/riccardoalv/omini/commit/e9c6632cd6edc760c215b760717147c48047885f))
* **web:** show the spanning tree root in a switch's panel ([7e7c247](https://github.com/riccardoalv/omini/commit/7e7c24752fed6b69732a5be1b5ffce1f7f056016))


### Bug Fixes

* **api:** anonymize every name in the diagnostics file's alerts ([2a19624](https://github.com/riccardoalv/omini/commit/2a196241344e9e82666f0e02ac5192998cf7d762))
* **api:** name a device no longer on the map on the Internet screen ([3f884f9](https://github.com/riccardoalv/omini/commit/3f884f904e6fbeca4e577d2ca966b558a8f1f5af))
* **collector:** a device first keyed by its address keeps its history too ([37dd694](https://github.com/riccardoalv/omini/commit/37dd694747f2efb4838d630a316f6d49bbd719d6))
* **collector:** change a site's VLAN once, not through every switch ([8c13264](https://github.com/riccardoalv/omini/commit/8c13264bf16d108779a81bbdfe054984103e7d88))
* **collector:** count a site change made through another device as Omini's ([2338348](https://github.com/riccardoalv/omini/commit/2338348f812dcd299a739b6ea479e11a2ce47a97))
* **collector:** do not count Omini's own port toggles as link flapping ([8a66498](https://github.com/riccardoalv/omini/commit/8a66498d32d909a0c7e2f6df62b3a8026e92a4a7))
* **collector:** keep a change made for a device in that device's history ([f08d696](https://github.com/riccardoalv/omini/commit/f08d6964d4de953ad5589d4f68dd6558d357e3d6))
* **collector:** keep a device's history when its key changes ([f8c6f78](https://github.com/riccardoalv/omini/commit/f8c6f784d65db72ddf6e8e762fc5138f311255a1))
* **collector:** keep a repeated refusal once and back off the retries ([a595029](https://github.com/riccardoalv/omini/commit/a595029fe7ccb0d96f973a5632d0ff838d267d05))
* **collector:** never move Omini's own host to another address ([9504819](https://github.com/riccardoalv/omini/commit/9504819a34f41d73b76c5ddf23fd9c72786a7e90))
* **collector:** reserve addresses only on a router that serves DHCP ([344e69c](https://github.com/riccardoalv/omini/commit/344e69c0e6dbb54e6c42fd73b070f4eb99135f35))
* **collector:** stop a Wi-Fi change at the first device that refuses ([8446514](https://github.com/riccardoalv/omini/commit/8446514df1d3011fe70cf78a7d19bce77fbe0fba))
* **collector:** wake a device that left the map, from the device list ([5e612de](https://github.com/riccardoalv/omini/commit/5e612defb60c76bcd506e15607b9cef108cc0667))
* **insights:** no dhcp_no_offer on a network whose DHCP is off ([f577c77](https://github.com/riccardoalv/omini/commit/f577c7753c6b0b06854c7d3969a3d21cf6a023de))
* **insights:** tell a gateway that is down without a WAN interface ([7fdaf79](https://github.com/riccardoalv/omini/commit/7fdaf798f6096368f20b05ed6674b9fb395f5343))
* **schema:** a radio's channel width is a range, not an integer enum ([99e5625](https://github.com/riccardoalv/omini/commit/99e5625eba6d04b23a2ddd192c809101b03736ad))
* **schema:** a radio's channel width is a range, not an integer enum ([f2d2bfe](https://github.com/riccardoalv/omini/commit/f2d2bfee3648fa6346220966c2da4bc5fd866519))
* **schema:** a radio's channel width is a range, not an integer enum ([1633fab](https://github.com/riccardoalv/omini/commit/1633fab66572cd320b8f353bd176d742ce14ec0f))
* **schema:** decode a radio's width ([ad6fe9d](https://github.com/riccardoalv/omini/commit/ad6fe9da1a7f5a3436f5cb980ecd0f68324ebf55))
* **snmp:** key a device without LLDP by a MAC that does not move ([1074370](https://github.com/riccardoalv/omini/commit/1074370c9a2edbfa8a2565a0358f5ce9d1aaaa7e))
* **snmp:** map Q-BRIDGE filtering database ids to their VLAN ([1b11c86](https://github.com/riccardoalv/omini/commit/1b11c86a94884a7447331ee9a60cfefa5599ffea))
* **snmp:** read a trunk without a native VLAN right and check the PVID ([8542d07](https://github.com/riccardoalv/omini/commit/8542d076bfc6793743a9deb67edaf68f90ebc4bb))
* **snmp:** read the memory when the processor table fails ([6745410](https://github.com/riccardoalv/omini/commit/6745410131b6c3e651f0ed255db3ce7998604fc8))
* **snmp:** refuse a port name several interfaces share ([49d7b54](https://github.com/riccardoalv/omini/commit/49d7b54a6967c5b466e437a2ed00ceed8ba3f476))
* **snmp:** refuse VLAN changes on ports that are not bridge ports ([fbd8deb](https://github.com/riccardoalv/omini/commit/fbd8deb6825c6b7f8fbde6fc74dace9890570ad3))
* **snmp:** refuse VLAN names longer than dot1qVlanStaticName holds ([76a2a2e](https://github.com/riccardoalv/omini/commit/76a2a2eaa145e84df3c60411f1baae2442f3b79e))
* **snmp:** retry the probe and keep a host's credential when it fails ([debd9b1](https://github.com/riccardoalv/omini/commit/debd9b1787184deeb4bed7f8a61a7c819c45edd2))
* **snmp:** say the scan's write credentials change VLANs too ([ce7a5f7](https://github.com/riccardoalv/omini/commit/ce7a5f7fcfca3ad7ea046c2aa93b3adabf8ae46b))
* **web:** ask once before pausing a device's internet ([3bdd186](https://github.com/riccardoalv/omini/commit/3bdd18647aa851254bc09ec5ca007670247c8fc1))
* **web:** bring the TS types in line with the model and flag a hot module ([83aaa40](https://github.com/riccardoalv/omini/commit/83aaa4050b9659436e763360266c8588614eaabc))
* **web:** translate the built-in integrations' missing form fields ([76137da](https://github.com/riccardoalv/omini/commit/76137dad5640615d9b7082e6b4fc776fefda892d))
* **web:** translate the spanning tree role of a port ([493a928](https://github.com/riccardoalv/omini/commit/493a9282b2f938dd896ccd66af2fd1c52d171411))


### Performance

* **snmp:** compile a profile's sys_descr expression once ([7406226](https://github.com/riccardoalv/omini/commit/740622671a82761b8f4968f0375e5338052747da))

## [1.6.2](https://github.com/riccardoalv/omini/compare/v1.6.1...v1.6.2) (2026-10-14)


### Bug Fixes

* a DNS server is slow after two slow checks in a row ([c486c8e](https://github.com/riccardoalv/omini/commit/c486c8e507c1d7744c4aee01b65faa279271c6b5))

## [1.6.1](https://github.com/riccardoalv/omini/compare/v1.6.0...v1.6.1) (2026-10-14)


### Bug Fixes

* alert DNS around the local server only when the outside server answers ([6a9f10a](https://github.com/riccardoalv/omini/commit/6a9f10a31fc78521065c038585de00394b576696))

## [1.6.0](https://github.com/riccardoalv/omini/compare/v1.5.0...v1.6.0) (2026-10-14)


### Features

* Internet screen, where each device goes and alerts from flows ([4f9bc44](https://github.com/riccardoalv/omini/commit/4f9bc44c46b98ff22f8b66f0a6c0845c9f2df627))
* the Internet screen is a dashboard with a world map ([0b4e010](https://github.com/riccardoalv/omini/commit/0b4e0104e7d3b402ce26409a19170ead2ce2e3c8))


### Bug Fixes

* DNS lookups of a merged device name destinations; the devnet exports NetFlow ([97e51cd](https://github.com/riccardoalv/omini/commit/97e51cd093c000d7f9b99be8a437d21095bfdd28))

## [1.5.0](https://github.com/riccardoalv/omini/compare/v1.4.0...v1.5.0) (2026-10-13)


### Features

* anonymized diagnostics file and a guide for testers ([a2d0722](https://github.com/riccardoalv/omini/commit/a2d0722253d15894b74a568a7255bde3a2def8b3))

## [1.4.0](https://github.com/riccardoalv/omini/compare/v1.3.2...v1.4.0) (2026-10-12)


### Features

* blocks set by hand on the router show as paused ([529c69d](https://github.com/riccardoalv/omini/commit/529c69d48c3fb1f464d49d972b266c4e08942e57))

## [1.3.2](https://github.com/riccardoalv/omini/compare/v1.3.1...v1.3.2) (2026-10-12)


### Bug Fixes

* pausing cuts IPv6 too, "Reserve" wording and no more pinning ([ac20b16](https://github.com/riccardoalv/omini/commit/ac20b16630aa847baad3104da565f8122dbae003))

## [1.3.1](https://github.com/riccardoalv/omini/compare/v1.3.0...v1.3.1) (2026-10-12)


### Bug Fixes

* a device read by SNMP and a plugin changes through both ([e806694](https://github.com/riccardoalv/omini/commit/e806694e69aad35475955be341f845802d882787))
* no DHCP answer to Omini is a warning while the router serves DHCP ([6556457](https://github.com/riccardoalv/omini/commit/6556457800f265a684f6e0b5ada6f2d2fec3823d))

## [1.3.0](https://github.com/riccardoalv/omini/compare/v1.2.0...v1.3.0) (2026-10-12)


### Features

* homelab tools: fixed IPs, DNS names, wake, pause, STP, LAGs and backups ([c5f7381](https://github.com/riccardoalv/omini/commit/c5f73818d7eb06cedb89daed3460237ee1a7d349))
* VLAN changes on every switch and router plugin ([77488f4](https://github.com/riccardoalv/omini/commit/77488f435522e6fe5f668996b0e33d3971acc98e))

## [1.2.0](https://github.com/riccardoalv/omini/compare/v1.1.0...v1.2.0) (2026-10-12)


### Features

* configuration changes, DNS and DHCP checks ([383fae9](https://github.com/riccardoalv/omini/commit/383fae960ec0d4f619d6c2b4c1bee3d33de6ca04))
* VLANs as whole networks, the map's menu and network areas ([0c4dc43](https://github.com/riccardoalv/omini/commit/0c4dc43a0d1f340072cf002325465198663780e8))
* VLANs for the whole network ([35bfa1a](https://github.com/riccardoalv/omini/commit/35bfa1a7d4f12caeed98dd7b4086b95bd584b4ae))

## [1.1.0](https://github.com/riccardoalv/omini/compare/v1.0.0...v1.1.0) (2026-10-09)


### Features

* **auth:** change the admin password, reset a forgotten one ([466dea9](https://github.com/riccardoalv/omini/commit/466dea97239c0a1eb3189f09acbd1adc0f30c64f))
* **integrations:** one "Run a round now" button instead of one per integration ([bed74af](https://github.com/riccardoalv/omini/commit/bed74af5b51474aa8b54db05cacf252bf3333c60))
* **netscan:** DHCP fingerprints tell a device's operating system ([27fd804](https://github.com/riccardoalv/omini/commit/27fd80423a0b093680d410939b39cd858f1ff59e))
* **notify:** cards that describe the device, with what to do and a link ([a3bfb3a](https://github.com/riccardoalv/omini/commit/a3bfb3aac4ad8ec34eebe12e8e48d4bb16c46db0))
* **snmp:** LLDP capabilities, link aggregation members and per-VLAN MAC tables ([67713f5](https://github.com/riccardoalv/omini/commit/67713f5aab6865cf42060bdb375f67af164d1923))
* **topology:** a client stays where it was last seen for sure ([2c290d0](https://github.com/riccardoalv/omini/commit/2c290d02b9556aaf2f4683a52cf99d0fa396181d))


### Bug Fixes

* **alerts:** an update with its version but no count says just the version ([ecf7c82](https://github.com/riccardoalv/omini/commit/ecf7c82281e974e4d96a8acd138944dbe3cc502c))
* **alerts:** no fast Ethernet alert for devices made with a 100 Mbps port ([d7ef03c](https://github.com/riccardoalv/omini/commit/d7ef03c184a885a49b7b25c99c8f1c87a417bb57))
* **topology:** desk phones, lldpd servers, CARP MACs and modems in WAN VLANs ([96ec1ca](https://github.com/riccardoalv/omini/commit/96ec1ca77d75b4ba8f0699695380a96ff52b633e))

## [1.0.0](https://github.com/riccardoalv/omini/compare/v0.3.0...v1.0.0) (2026-10-08)


### Features

* **collector:** collect in rounds, one integration at a time, edge to center ([f7f1373](https://github.com/riccardoalv/omini/commit/f7f137390f4126e69a48354dd548267acef04156))
* **collector:** remembered switch ports survive restarts ([7a47524](https://github.com/riccardoalv/omini/commit/7a4752493da9a65dfda738e5f4ee581c8efd530b))
* **flows:** who talks to whom, from NetFlow, IPFIX and sFlow ([daa1fcc](https://github.com/riccardoalv/omini/commit/daa1fcc9f54011f2eeaf4f65c303e3c4c75303d6))
* insights, traffic history, presence timeline and animated flow ([4975226](https://github.com/riccardoalv/omini/commit/4975226f13a0bec9ab0b0667757c2171be27e91e))
* **insights:** ten new alerts and popups of the open ones ([595f238](https://github.com/riccardoalv/omini/commit/595f23802280ba472cef013e76c96652ae88b081))
* **map:** a click on a Wi-Fi network folds or unfolds its clients ([cd193ca](https://github.com/riccardoalv/omini/commit/cd193caee2b2640ff869cf34fa981082868ae4ba))
* **map:** a Wi-Fi client's panel shows its traffic even while idle ([456add8](https://github.com/riccardoalv/omini/commit/456add8416687b4d79052d09a5e064c172d66668))
* **map:** automatic areas per VLAN and subnet, any area color, no link animation ([0f07b20](https://github.com/riccardoalv/omini/commit/0f07b20bbd94864a791761f6cc80410232497a32))
* **map:** draw.io export, VLAN view, device extras and six new plugins in the store ([5323270](https://github.com/riccardoalv/omini/commit/5323270c791071b58fadaeeae74d282bdf8fbb07))
* **map:** every node can fold its children ([02df595](https://github.com/riccardoalv/omini/commit/02df595ff806c8eebaaa6782e2eefd9d75c1eb56))
* **map:** export dialog (format, theme, orientation); layout keeps grids and areas in order ([d0587cc](https://github.com/riccardoalv/omini/commit/d0587cc4bd582c4675701cda721861936a469c98))
* **map:** export the map as PNG, SVG or JSON ([0654719](https://github.com/riccardoalv/omini/commit/0654719edd31d768c0e9ae504e5ba80583d76a53))
* **map:** exported images show every group and area expanded ([dd9305e](https://github.com/riccardoalv/omini/commit/dd9305ef06cdf6e96f08404fa921061169255d15))
* **map:** fold a host's VMs like clients ([5c5db4c](https://github.com/riccardoalv/omini/commit/5c5db4cef7c02ba106720066f15725bef23350d8))
* **map:** frame what the VLAN filter highlights; two alerts as popups ([0c6feb1](https://github.com/riccardoalv/omini/commit/0c6feb14bf44b27af9add555e4b681a88015ed2d))
* **map:** nicer area color picker; hide empty scanned networks ([5135530](https://github.com/riccardoalv/omini/commit/513553018dfb02214136488416262600c5579d9c))
* **map:** siblings grouped by the port or Wi-Fi network they use ([3b8d8b8](https://github.com/riccardoalv/omini/commit/3b8d8b8e40bbc5bff7ef31cd665949daea0b7179))
* **map:** strict areas; VLANs and subnets as a filter ([0d3ef1a](https://github.com/riccardoalv/omini/commit/0d3ef1ac3fae838ef194e0d61a293e819106f7fd))
* **map:** tidy tree layout, pills clear of badges and areas, hide areas ([ad3c532](https://github.com/riccardoalv/omini/commit/ad3c5326036a1d9e87eca0746d0e240e4d26780e))
* **map:** Wi-Fi clients show their network and band on themselves ([5e07209](https://github.com/riccardoalv/omini/commit/5e07209d0f4cedfb73b0ce9f846717930c4777ce))
* **map:** Wi-Fi clients show their traffic, band and link rate ([810ee3b](https://github.com/riccardoalv/omini/commit/810ee3b32c10bb6c77849dd6c04d4039958ed626))
* **map:** Wi-Fi networks as mini nodes between an access point and its clients ([3838b7d](https://github.com/riccardoalv/omini/commit/3838b7df87831c71e4578e1681ffa2d8c58631d4))
* **netscan:** also scan the networks the routers report ([dd27a14](https://github.com/riccardoalv/omini/commit/dd27a14c09142d8fd4dc93dba5bb65250a0e2967))
* **netscan:** check what discovery can do here and say how to fix it ([f3a43d3](https://github.com/riccardoalv/omini/commit/f3a43d345c365dedcb997b176fdc6c5683e9ecd6))
* **notify:** notifications grouped by device, as cards in chat services ([89dc12a](https://github.com/riccardoalv/omini/commit/89dc12a1be3902f8f3726f5526701a1193742754))
* **notify:** send alerts to a webhook, Telegram or e-mail ([10c9188](https://github.com/riccardoalv/omini/commit/10c91880fadeda8b5ae119933ce3208017b7ce54))
* **plugins:** remote store index, store screen and the review process ([6942004](https://github.com/riccardoalv/omini/commit/6942004a4d32055d0ed0906bd796d0022db614a9))
* **schema:** number of CPUs and memory sizes in the device panel ([d15f7f9](https://github.com/riccardoalv/omini/commit/d15f7f9e3dc24afab76812a99db30c0b357ac62f))
* **schema:** VLANs, transceiver diagnostics, services, VPN peers, DHCP pools, firewall states ([2c53a80](https://github.com/riccardoalv/omini/commit/2c53a80d683d8b1040592311f2dd6faf080948f6))
* **snmp:** SNMP v3 and YAML vendor profiles ([2ce42d0](https://github.com/riccardoalv/omini/commit/2ce42d04b0eb57aa8d1fdc63091f24f5bdf95a39))
* **topology:** find an unmanaged switch beside a device that lists its clients ([eebd715](https://github.com/riccardoalv/omini/commit/eebd715634b84ef6e7f45cfd0a1f53a0be0adff4))
* **ui:** integration logos in the list and the store; a sharp Mercusys logo ([0981268](https://github.com/riccardoalv/omini/commit/0981268a07d98ee0688bf6d428536503af7aa163))
* **web:** Flows in the menu only while on; the store back to a modal ([5e8aa73](https://github.com/riccardoalv/omini/commit/5e8aa733fd70227c9699eb182e3a22d7ab919144))
* **web:** rename the Insights screen to Alerts ([2248672](https://github.com/riccardoalv/omini/commit/2248672bb5156948d15ccbd5e9b0a754817d4290))
* **web:** settings with a section menu and notifications as accordions ([805b48a](https://github.com/riccardoalv/omini/commit/805b48a0ab5929a587ac0de0957513a35d275620))
* **web:** the device list is a drawer on the map ([e8524d2](https://github.com/riccardoalv/omini/commit/e8524d22021558adcaadd08680948520321d9c06))


### Bug Fixes

* **classify:** TVs, speakers, IP phones, UPSes and stricter app titles ([f55d232](https://github.com/riccardoalv/omini/commit/f55d23285e98b972f20d7e1ad9ed7c7508951aa0))
* **collector:** a rate above what the port carries is a glitch, not traffic ([4ec3e55](https://github.com/riccardoalv/omini/commit/4ec3e55b1e81dcdb6182b46d57d6576a7c9a5811))
* **collector:** presence and flow listeners follow the round interval ([6ef1126](https://github.com/riccardoalv/omini/commit/6ef112643949db88a38accc3a370a60c00ecf759))
* **collector:** the device list says where devices are connected ([608bfdd](https://github.com/riccardoalv/omini/commit/608bfdd95bdb485f5e37c8b1c291203bf50807e1))
* **map:** a client's traffic badge no longer covers its name ([fa71540](https://github.com/riccardoalv/omini/commit/fa715404f47f8b58b7fa9ce18f1354e9d50d3073))
* **map:** a second parent's pill no longer covers the tree link's ([761f81a](https://github.com/riccardoalv/omini/commit/761f81acecc212da0518babce01eb32b6026b50a))
* **map:** an area follows its members into a bubble ([a4f9d64](https://github.com/riccardoalv/omini/commit/a4f9d64a6a868f1a05f9c7240e8426918bff42c7))
* **map:** areas keep their devices together; no false new devices ([a7d4ee7](https://github.com/riccardoalv/omini/commit/a7d4ee7d2b834a06dc5405c1ff7e75e2770fb3d1))
* **map:** expanding a group no longer opens the side panel ([357feca](https://github.com/riccardoalv/omini/commit/357fecaa64212e14a1b528e800d19f1cb5f153dc))
* **map:** exported images frame the whole map instead of a shifted, cut one ([e8a4d83](https://github.com/riccardoalv/omini/commit/e8a4d830992eef554e1984a0b4e94321a3dd4f78))
* **map:** exported SVG draws the wires as lines, not black triangles ([f27608a](https://github.com/riccardoalv/omini/commit/f27608ae2fa5567b7447346394533637b788ab8a))
* **map:** one machine, one node; no group button in the panel ([8cd136e](https://github.com/riccardoalv/omini/commit/8cd136eb93ccf39cd0d4de712d541e4acf406ada))
* **map:** steady switch uplinks; access points collapse with their Wi-Fi networks ([b46aed4](https://github.com/riccardoalv/omini/commit/b46aed49411f8a11e5c216d5a0631c50c54b18bf))
* **map:** the layout keeps the siblings' order, so Wi-Fi groups never interleave ([7dd34a3](https://github.com/riccardoalv/omini/commit/7dd34a30d5e1a7ae78f4f0e103ee4550ebfc2fd2))
* **map:** the VLAN/subnet filter no longer lights the LAN under the firewall ([be1ea6c](https://github.com/riccardoalv/omini/commit/be1ea6c88098be19c27b2dc7c28d0b4f28ea9d2a))
* **map:** traffic badges on top of every node, compact on clients ([9dc7011](https://github.com/riccardoalv/omini/commit/9dc7011cb1a02b3392556c8ea3c702b505bacc6c))
* **map:** Wi-Fi network nodes can be dragged ([b2ac45a](https://github.com/riccardoalv/omini/commit/b2ac45aa111e45b49b241e6ab703d7373b517cb9))
* **map:** Wi-Fi network nodes wide enough for their names ([2befabb](https://github.com/riccardoalv/omini/commit/2befabb7ca4b07f23acfe7158a81bf090594fd0d))
* **topology:** a device without MACs is the machine its address points to ([75267ca](https://github.com/riccardoalv/omini/commit/75267ca9cba7a8406a09b978d9545177cc4c69a2))
* **topology:** a MAC only a switch table has, behind an AP that does not list it, is not shown ([3c57867](https://github.com/riccardoalv/omini/commit/3c5786774f9955a96e8f9a54c43035b43a6e37ff))
* **topology:** guests reported by an integration hang under their host, which stays on its port ([649ac54](https://github.com/riccardoalv/omini/commit/649ac54f7d29c21d79c51b143c7a6db84698ca39))
* **topology:** integration devices never float apart; brand icon for unknown types ([28c925c](https://github.com/riccardoalv/omini/commit/28c925c6a2e25eecb6f9e41574bec86449d0df25))
* **topology:** nested routers, firewall VMs seen by their host, quieter alerts ([09fb05e](https://github.com/riccardoalv/omini/commit/09fb05e262ded7dcbc38f3656a59258696f1be3a))
* **topology:** port-channels, site-to-site VPNs, routed scans, LLDP phones ([5730746](https://github.com/riccardoalv/omini/commit/5730746ea9078a63100201a7ece1cf98e66734ec))
* **web:** popups clear of the device panel; updates without a version ([0d40547](https://github.com/riccardoalv/omini/commit/0d405475fe9927d6773ab1026af19950a464e94b))


### Documentation

* banner, screenshots and a README for the first official release ([8e80c5b](https://github.com/riccardoalv/omini/commit/8e80c5beb991ddec66fb21c74b0f766810580a75))

## [0.3.0](https://github.com/riccardoalv/omini/compare/v0.2.0...v0.3.0) (2026-10-07)


### Features

* Horaco and Mercusys in the store; clients keep their switch port while the switch forgets them ([f854e60](https://github.com/riccardoalv/omini/commit/f854e609751c423029a95a9cef376a58c32a73fb))
* **map:** port options on click, port names mid-link, switch uplinks from MAC tables ([1421e7b](https://github.com/riccardoalv/omini/commit/1421e7b2f40b809a55a386e5c848ad51b15c7453))

## [0.2.0](https://github.com/riccardoalv/omini/compare/v0.1.2...v0.2.0) (2026-10-07)


### Features

* **map:** resizable device panel with a clearer layout; quick nmap versions by default ([937c5e5](https://github.com/riccardoalv/omini/commit/937c5e5d693d0356866723d91e0d9a287b3c8c45))
* **map:** show the port's name next to the link speed ([50ebe30](https://github.com/riccardoalv/omini/commit/50ebe306a201910c3294b0b62a8f57c527721957))
* **nmap:** configurable device scan, light by default ([9fd084e](https://github.com/riccardoalv/omini/commit/9fd084ef089676107f73116c05fdfe6cb94e47ac))
* **nmap:** deep scan from the device panel (top 1024 ports, -sV -sC, OS with permission) ([6f997b2](https://github.com/riccardoalv/omini/commit/6f997b2b853c2343990f193d109d56006436d273))
* scan one device with nmap from its panel ([ba2a447](https://github.com/riccardoalv/omini/commit/ba2a4478610922b7d0a67bc503743d6311dfd0cd))
* **schema:** system health — load, swap, disks, temperatures and pending updates ([3da871c](https://github.com/riccardoalv/omini/commit/3da871c05a13587bf206d2a8334b86e8a2418dd8))

## [0.1.2](https://github.com/riccardoalv/omini/compare/v0.1.1...v0.1.2) (2026-10-07)


### Bug Fixes

* nmap no longer adds a second gateway; traffic shown on devices ([036da53](https://github.com/riccardoalv/omini/commit/036da53cf27cc01353356d51452eceaae6f0fc29))

## [0.1.1](https://github.com/riccardoalv/omini/compare/v0.1.0...v0.1.1) (2026-10-07)


### Bug Fixes

* **map:** the map screen was blank since areas could be collapsed ([d8f41b1](https://github.com/riccardoalv/omini/commit/d8f41b19e8efd3e4b4e884fd62a66da56df54586))

## 0.1.0 (2026-10-07)


### ⚠ BREAKING CHANGES

* the "snmp" integration type and POST /api/discovery/scan no longer exist.

### Features

* a single network scan; "Add integration" opens the store ([bfe7a30](https://github.com/riccardoalv/omini/commit/bfe7a3005a5073a7f4fca8331465155fe7dfa2e3))
* add the omini server command ([c88d056](https://github.com/riccardoalv/omini/commit/c88d056ab817c6ba6f81be8b5fb4bb0a37f3efcf))
* **api:** add HTTP API and serve the embedded web UI ([7229ef3](https://github.com/riccardoalv/omini/commit/7229ef3a91e13f0757ee174f984ad4846f0af3ce))
* **api:** detect devices' web interfaces ([6f54559](https://github.com/riccardoalv/omini/commit/6f545599515b7508df127b5bda07f12cd0214a86))
* **api:** include the device role in the inventory ([5919e2c](https://github.com/riccardoalv/omini/commit/5919e2cdbc908d72fd2ff9d4619ca92d698139c1))
* **api:** run an integration now, skipping its caches ([62c9172](https://github.com/riccardoalv/omini/commit/62c9172ea166058f11c6869e775e8692741d1928))
* app nodes with catalog icons, broader port scan and a steady map ([a8abd74](https://github.com/riccardoalv/omini/commit/a8abd7446c5a261f18c5f0cd8eef0ab093f1bf7b))
* areas follow their devices; UI language saved per user ([531d8ea](https://github.com/riccardoalv/omini/commit/531d8ea96c43d30f1d0c035ed4a62fcb7aa2d2c0))
* **auth:** add first-run admin setup and session login ([87186c0](https://github.com/riccardoalv/omini/commit/87186c053a6a8b88211ee93ad23bfe3aa3684dfb))
* classify map nodes and let users correct the result ([45a279a](https://github.com/riccardoalv/omini/commit/45a279aa097f7682cf2f810ced8e343c04e4a18d))
* **classify:** identify device type, OS, brand and homelab software ([48d685a](https://github.com/riccardoalv/omini/commit/48d685a2d390617d987e753f3e460eb7bf9b3d96))
* **classify:** recognize air conditioners and count scanned hosts in status ([ecac310](https://github.com/riccardoalv/omini/commit/ecac3105e567ba82af63798789ae6ffdc689b567))
* **classify:** recognize solar inverters ([5969907](https://github.com/riccardoalv/omini/commit/5969907a012e98865fa569a22ed463e629c64237))
* collection interval per integration ([c102a50](https://github.com/riccardoalv/omini/commit/c102a5024cc6f1eeb5e7f7af98a525ddcb00816d))
* **collector:** place Proxmox VMs under the Proxmox host ([29f2981](https://github.com/riccardoalv/omini/commit/29f2981319476d024387156769210a9551143bd9))
* **collector:** poll integrations and maintain the map state ([84f474e](https://github.com/riccardoalv/omini/commit/84f474e8c6ff4bc78fabe9daf8c7f08ca851648e))
* **demo:** add a fictional homelab network ([c6b6c30](https://github.com/riccardoalv/omini/commit/c6b6c3016433a98d9472974e443a25e8ecc9cd53))
* Docker image, published with each release ([5bcd3f4](https://github.com/riccardoalv/omini/commit/5bcd3f47f715ac4a5083bee5fca4a1d82b1e8300))
* hide or delete a device from its panel ([73827d4](https://github.com/riccardoalv/omini/commit/73827d47991389c79af3aee648a2b801905fe9fe))
* **integration:** normalize configs and protect secret fields ([4789dd2](https://github.com/riccardoalv/omini/commit/4789dd2f3e74c51bac93d17048bdaccc8e8c22ce))
* live traffic on the map ([65ee2b0](https://github.com/riccardoalv/omini/commit/65ee2b07691cbf248dc9284b68175df7e53037c9))
* **map:** a node in an area brings its children ([2c5c53e](https://github.com/riccardoalv/omini/commit/2c5c53e281acb6a792f0ee2b5a87bdc62c1fb0d1))
* **map:** collapse an area into a bubble ([9b1f2b9](https://github.com/riccardoalv/omini/commit/9b1f2b93ee5c944349bcca9e1dc3ae3842de2301))
* **map:** compact top-down view with leaf children in grids ([44296a1](https://github.com/riccardoalv/omini/commit/44296a183b29c622d0a4cc7c26226a1e02af588d))
* **map:** lay out areas as boxes so other devices stay out ([91bac5f](https://github.com/riccardoalv/omini/commit/91bac5f24875c0ab53e6ecd63df342d297c72815))
* **map:** named areas to group devices; expand without overlaps ([e680118](https://github.com/riccardoalv/omini/commit/e6801183c9258e72694642e0400675f456583fce))
* **map:** port front view, CPU/memory bars and link speeds ([a598ba5](https://github.com/riccardoalv/omini/commit/a598ba527b6770fc96fb1132fb30fc4e505968e6))
* **netscan:** collect web titles, SSH banners, ping TTL and this server's OS ([7e1a7db](https://github.com/riccardoalv/omini/commit/7e1a7db486443e54572ad4f43368e1fdef73938e))
* **netscan:** find every device on the network with zero setup ([65d2774](https://github.com/riccardoalv/omini/commit/65d2774d2713d1d2f700ded0abd498e33eec462b))
* **netscan:** make every discovery method configurable ([6895744](https://github.com/riccardoalv/omini/commit/68957449bb943b0d44da37fe243ed068f571a155))
* nmap integration and device model names ([676f93f](https://github.com/riccardoalv/omini/commit/676f93faa4b82b26cb1d8393bc2d6e0089023767))
* **oui:** embed the IEEE MAC vendor database ([f661e2b](https://github.com/riccardoalv/omini/commit/f661e2bbd415384b389de75831f33f186256655a))
* **panel:** describe ports ([5e92d51](https://github.com/riccardoalv/omini/commit/5e92d51a65246061e815fefadeee2e738f8274f9))
* **plugins:** install any GitHub repository; curated catalog with OPNsense ([6558e78](https://github.com/riccardoalv/omini/commit/6558e7834fb4f75c44e07a0ae0bbcba577884601))
* **plugins:** Python plugin runtime, SDK and plugin management ([c739fa2](https://github.com/riccardoalv/omini/commit/c739fa2fb18a46b83a53bb223520dc8c15c94fd1))
* scan the network automatically on first start ([605f0ef](https://github.com/riccardoalv/omini/commit/605f0efed6aaeafe47c2d08bb2c9ec3008c11056))
* **schema:** add data contract and code generation ([00b7232](https://github.com/riccardoalv/omini/commit/00b7232d983a2b8f25a2c2677dabdb6fb3f5d2d0))
* **schema:** add hosts observed by integrations ([61d6061](https://github.com/riccardoalv/omini/commit/61d60611b5d12f3ea7ff43592787e61c875d7b3f))
* **schema:** group form fields into sections ([24a376b](https://github.com/riccardoalv/omini/commit/24a376bc59e5e5992641bf8243d7d6fc500a2873))
* **schema:** record web titles, banners and TTL of hosts ([c1ca073](https://github.com/riccardoalv/omini/commit/c1ca073ad503c0855d009c09bc773791ba89d7e6))
* **secret:** encrypt sensitive config values at rest ([45ba65c](https://github.com/riccardoalv/omini/commit/45ba65cb5761f16a8862ded7a76c1791519116ab))
* SNMP is a method of the network scan; integration names are fixed ([5509a8d](https://github.com/riccardoalv/omini/commit/5509a8dfe4eb3196658cce6e4ff8a169e0ade649))
* **snmp:** add generic SNMP v2c integration ([82acdbe](https://github.com/riccardoalv/omini/commit/82acdbe125b1ca0327550af93fec8d607df95b7d))
* **snmp:** discover SNMP devices in a subnet ([55649c7](https://github.com/riccardoalv/omini/commit/55649c7c6daa84bdaaf5353692cc3c8ba65a3035))
* **store:** add SQLite store with migrations ([6fb4e13](https://github.com/riccardoalv/omini/commit/6fb4e13bdb8a735343ee0bf4e3b02f9b491c5226))
* **topology:** build the network map from collected devices ([88c3dce](https://github.com/riccardoalv/omini/commit/88c3dce590ff75d2c8ab45c37b8b52c8537a76be))
* **topology:** place scanned hosts and keep every integration's data ([b8b8323](https://github.com/riccardoalv/omini/commit/b8b8323ec81c35fc4223e4bff71ac38110b8dd66))
* WAN nodes above the firewall; RJ45 and SFP ports ([ad8b6c2](https://github.com/riccardoalv/omini/commit/ad8b6c2a077791ca3b6fab30679958f799626a1a))
* **web:** add the web UI ([4472dc1](https://github.com/riccardoalv/omini/commit/4472dc1d2dddb008c9433239c7f55868d4c16fbf))
* **web:** device icons with brand logos and identification panel ([24d1222](https://github.com/riccardoalv/omini/commit/24d1222f0b0e4915c3bed675081543997b9da0a0))
* **web:** expandable integrations, device names, letter badges, context menu ([98082a3](https://github.com/riccardoalv/omini/commit/98082a3818bc294fbc2d528debb582a3aa6a9ea3))
* **web:** open device web interfaces, map orientation and sidebar ([9c7a806](https://github.com/riccardoalv/omini/commit/9c7a80680f7e473b903dd9a76bfcfbef59a9876e))
* **web:** plugin store modal with search and add by URL ([ba52d25](https://github.com/riccardoalv/omini/commit/ba52d25785bb6d61d62389711058b697524671e9))
* **web:** run now, hide offline devices, drop discovery dialog and letter badges ([1d74b5b](https://github.com/riccardoalv/omini/commit/1d74b5b200c53b35c02f939424fc103d6a1223c0))
* **web:** scan button on the empty map, switches for yes/no fields ([7fc67d2](https://github.com/riccardoalv/omini/commit/7fc67d283328da3dc77632dbdfde544442b6a34c))
* **web:** show/hide toggle for secrets; blank keeps the saved one ([bc26cfa](https://github.com/riccardoalv/omini/commit/bc26cfaa611ad19e60015bc93740de3f615af417))
* **web:** use a toggle switch to enable integrations ([fe75977](https://github.com/riccardoalv/omini/commit/fe75977a012f2a06e8de1f0c0cbb04c639144ed9))


### Bug Fixes

* **integrations:** accept addresses without https:// in URL fields ([9b2376a](https://github.com/riccardoalv/omini/commit/9b2376a07f8023628a47f989183975ece8d7774a))
* **map:** areas show in both orientations; hidden devices do not make the map look empty ([e637b1c](https://github.com/riccardoalv/omini/commit/e637b1c4152f30289f63025a2ea477d3b1bc804c))
* **map:** automatic nodes never land on dragged ones ([1cb7dbf](https://github.com/riccardoalv/omini/commit/1cb7dbf659b1adc92cf1e156b8dca16463950ee1))
* **map:** collapsing a node also hides the children of its children ([d4a4883](https://github.com/riccardoalv/omini/commit/d4a4883b9825f855e17fe864ac060c97a01b8c02))
* **model:** only parse textual MACs and flag group addresses ([628dcdd](https://github.com/riccardoalv/omini/commit/628dcdd2dcf67ef91e54d3a2ea72e7e0e560baa7))
* **models:** close the response before exiting in the generator ([5157a1f](https://github.com/riccardoalv/omini/commit/5157a1f7fbb93b99861e1aebaae49a2fc049625c))
* **panel:** web interface as an icon next to the device name ([fb9ccdf](https://github.com/riccardoalv/omini/commit/fb9ccdf68cdeb68169d7632ea4c513de6f96df20))
* **plugins:** run plugins with a relative data dir; install by URL from "Add integration" ([bd4afaf](https://github.com/riccardoalv/omini/commit/bd4afaffad4b5fa6c7c71985ddd37f75e92e77a7))
* **snmp:** set a stable device key and make timeouts configurable ([06e7877](https://github.com/riccardoalv/omini/commit/06e78770de131c7b42491e9b9b3a31d68be2828b))
