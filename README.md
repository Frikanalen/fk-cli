# Frikanalen command line utility

## Installation

```bash
sudo apt install golang
make
sudo make -e PREFIX=/usr install
```

## Install linter

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.43.0
```
