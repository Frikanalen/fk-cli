# Frikanalen command line utility


## Requirements

ffmpeg is only required for test video generation.

### Debian

```
sudo apt install golang ffmpeg
```

### MacOS
```
brew install golang ffmpeg
```

## Installation

```bash
make
sudo make -e PREFIX=/usr install
```

## Install linter

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.43.0
```
