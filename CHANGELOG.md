# Changelog

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
