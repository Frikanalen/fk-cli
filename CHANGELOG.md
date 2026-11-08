# Changelog

## [0.3.0](https://github.com/Frikanalen/fk-cli/compare/v0.2.0...v0.3.0) (2026-11-08)


### ⚠ BREAKING CHANGES

* command and flag spellings change throughout; see the README's "Command-line conventions". contrib/fkimport.py is updated to match, and its --org-id/--series-id become --org/--series.

### Features

* **archive:** queue catalogue maintenance from fk-cli ([#15](https://github.com/Frikanalen/fk-cli/issues/15)) ([4f312fe](https://github.com/Frikanalen/fk-cli/commit/4f312fe090c8a00ae81bd912db32a2890caa3bd7))
* **contrib:** import conference videos from media.ccc.de and the Debian archive ([#11](https://github.com/Frikanalen/fk-cli/issues/11)) ([c21573d](https://github.com/Frikanalen/fk-cli/commit/c21573da1cf6b9e545723d111248be9c016a3eee))
* **contrib:** remux imported recordings down to one video and audio track ([#13](https://github.com/Frikanalen/fk-cli/issues/13)) ([e71626d](https://github.com/Frikanalen/fk-cli/commit/e71626d4632a70a7cc8e49537a8bb0cc797ba1e8))
* make the command line consistent ([#18](https://github.com/Frikanalen/fk-cli/issues/18)) ([2236ddb](https://github.com/Frikanalen/fk-cli/commit/2236ddbdd9b446b05da5452b2e253fbb75c157cb))
* move from frikanalen.no to frikanalen.tv ([#19](https://github.com/Frikanalen/fk-cli/issues/19)) ([976490d](https://github.com/Frikanalen/fk-cli/commit/976490d2e279d068ccccb8e43b64bdd1ca5f6b8a))
* **series:** list an organization's series, and file new videos under one ([#10](https://github.com/Frikanalen/fk-cli/issues/10)) ([9f5258c](https://github.com/Frikanalen/fk-cli/commit/9f5258c767fae98d76cc9a94ce087fa763233924))
* upload from video create, and switch API environments ([#7](https://github.com/Frikanalen/fk-cli/issues/7)) ([fa929de](https://github.com/Frikanalen/fk-cli/commit/fa929de59099834b83e5d45e24077f5530158116))
* **video:** resumable ingest status that survives losing touch ([#12](https://github.com/Frikanalen/fk-cli/issues/12)) ([67bc8d0](https://github.com/Frikanalen/fk-cli/commit/67bc8d0261fd40c10e5056e5454966087c9d2219))
* **video:** show an animated rainbow progress bar for upload and ingest ([#9](https://github.com/Frikanalen/fk-cli/issues/9)) ([2c6a63a](https://github.com/Frikanalen/fk-cli/commit/2c6a63a06a28f019afa825ca119b9b3b4c2bdd9d))


### Bug Fixes

* **api:** pass video ID as int to regenerated client ([a01cafa](https://github.com/Frikanalen/fk-cli/commit/a01cafa4379b6476bf4d8c66da13018834abfe47))

## [0.2.0](https://github.com/Frikanalen/fk-cli/compare/v0.1.0...v0.2.0) (2026-08-21)


### Features

* now with login ([e151b68](https://github.com/Frikanalen/fk-cli/commit/e151b689659b057975ad4df24e813c99e9f3d08a))


### Bug Fixes

* missing reqs ([5e46e6d](https://github.com/Frikanalen/fk-cli/commit/5e46e6d63e92fcf52a391607b24a43c8adb2093e))
