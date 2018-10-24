<p align="center">vault-unseal -- auto-unseal utility for Hashicorp Vault</p>
<p align="center">
  <a href="https://travis-ci.org/lrstanley/vault-unseal"><img src="https://travis-ci.org/lrstanley/vault-unseal.svg?branch=master" alt="Build Status"></a>
  <a href="https://byteirc.org/channel/%23%2Fdev%2Fnull"><img src="https://img.shields.io/badge/ByteIRC-%23%2Fdev%2Fnull-blue.svg" alt="IRC Chat"></a>
</p>

## Table of Contents
- [Why](#why)
- [Solution](#solution)
- [Installation](#installation)
  - [Ubuntu/Debian](#ubuntudebian)
  - [CentOS/Redhat](#centosredhat)
  - [Manual Install](#manual-install)
  - [Build from source](#build-from-source)
- [Usage](#usage)
- [Contributing](#contributing)
- [TODO](#todo)
- [License](#license)

## Why

Depending on your use-case for Vault, you may or may not have opted for Vault
Enterprise. If you have not, auto-unseal functionality for on-prem is currently
only in enterprise (for cloud, it is now in the OSS version). If what you are
storing in vault isn't sensitive enough to require human intervention, you may
want to role your own unseal functionality. The problem with this is it is very
hard to do safely.

So, what do we need to solve? we want to auto-unseal a vault, by providing the
necessary unseal tokens when we find vault is sealed. We also want to make sure
we're sending notifications when this happens, so if vault was unsealed
unintentionally (not patching, upgrades, etc), possibly related to crashing or
malicious intent, a human can investigate at a later time (**not** 3am in the
morning).

## Solution

The goal for this project is to find the best way to unseal vault in a way that
doesn't compromise too much on security (a good balance between security and
ease of use/uptime), without the requirement of Vault Enterprise.

We do this by running multiple instances of vault-unseal (you could run one
on each node in the cluster). Each instance of vault-unseal is given a subset
of the unseal tokens. How many tokens you should configure for each vault-unseal
instance depends on how many are required to unseal your cluster -- you want to
give each node **just enough** tokens, that when paired with another vault-unseal
node, they can work together to to unseal the vault. Let's use the following
example:

   * `cluster-1` consists of 3 nodes:
      * `node-1`
      * `node-2`
      * `node-3`
   * `cluster-1` is configured with 5 unseal tokens (tokens `A`, `B`, and `C`), but
   3 are required to unseal a given vault node.
   * given there are 3 nodes, and 3 tokens are required:
      * vault-unseal on `node-1` gets tokens `A` and `B`.
      * vault-unseal on `node-2` gets tokens `B` and `C`.
      * vault-unseal on `node-3` gets tokens `A` and `C`.

With the above configuration:
   * Given each vault-unseal node, each node has two tokens.
   * Given the tokens provided to vault-unseal, each token (`A`, `B`, and `C`), there
   are two instances of that token across nodes in the cluster.
   * If `node-1` is completely hard-offline, nodes `node-2` and `node-3` should have
   all three tokens, so if the other two nodes reboot, as long as vault-unseal starts
   up on those nodes, vault-unseal will be able to unseal both.
   * If `node-2` becomes compromised, and the tokens are read from the config
   file (note: vault-unseal **will not start** if the permissions on the file aren't
   `600`), this will not be enough tokens to unseal the vault.
   * vault-unseal runs as root, with root permissions.

## Installation

Check out the [releases](https://github.com/lrstanley/vault-unseal/releases)
page for prebuilt versions. vault-unseal should work on ubuntu/debian,
centos/redhat/fedora, etc. Below are example commands of how you would install
the utility.

### Ubuntu/Debian

```bash
$ wget https://liam.sh/ghr/vault-unseal_0.0.1_linux_amd64.deb
$ dpkg -i vault-unseal_0.0.1_linux_amd64.deb
$ vault-unseal --help
```

### CentOS/Redhat

```bash
$ yum localinstall https://liam.sh/ghr/vault-unseal_0.0.1_linux_amd64.rpm
$ vault-unseal --help
```

Some older CentOS versions may require (if you get `Cannot open: <url>. Skipping.`):

```console
$ wget https://liam.sh/ghr/vault-unseal_0.0.1_linux_amd64.rpm
$ yum localinstall vault-unseal_0.0.1_linux_amd64.rpm
```

### Manual Install

```bash
$ wget https://liam.sh/ghr/vault-unseal_0.0.1_linux_amd64.tar.gz
$ tar -C /usr/bin/ -xzvf vault-unseal_0.0.1_linux_amd64.tar.gz vault-unseal
$ chmod +x /usr/bin/vault-unseal
$ vault-unseal --help
```

### Source

Note that you must have [Go](https://golang.org/doc/install) installed (`v1.11.1` required).

    $ git clone https://github.com/lrstanley/vault-unseal.git && cd vault-unseal
    $ make
    $ ./vault-unseal --help

## Usage

The default configuration path is `/etc/vault-unseal.yaml` when using `deb`/`rpm`.
If you are not using these package formats, copy the example config file,
`example.vault-unseal.yaml`, to `vault-unseal.yaml`.

```
$ ./vault-unseal --help
Usage:
  vault-unseal [OPTIONS]

Application Options:
  -l, --log-path=PATH    Optional path to log output to
  -c, --config=PATH      Path to configuration file (default: ./vault-unseal.yaml)

Help Options:
  -h, --help             Show this help message
```

## Contributing

Please review the [CONTRIBUTING](CONTRIBUTING.md) doc for submitting issues/a guide
on submitting pull requests and helping out.

## TODO

 - [ ] add option to use vault token/another vault instance to obtain keys (e.g. as long the leader is online)?
 - [ ] memory obfuscating/removing from memory right after unseal?

## License

```
MIT License

Copyright (c) 2018 Liam Stanley <me@liamstanley.io>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
