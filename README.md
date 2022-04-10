<!-- template:begin:header -->
<!-- do not edit anything in this "template" block, its auto-generated -->
<p align="center">vault-unseal -- vault-unseal -- auto-unseal utility for Hashicorp Vault</p>
<p align="center">
  <a href="https://github.com/lrstanley/vault-unseal/releases">
    <img alt="Release Downloads" src="https://img.shields.io/github/downloads/lrstanley/vault-unseal/total?style=flat-square">
  </a>


  <a href="https://github.com/lrstanley/vault-unseal/actions?query=workflow%3Arelease+event%3Apush">
    <img alt="GitHub Workflow Status (release @ master)" src="https://img.shields.io/github/workflow/status/lrstanley/vault-unseal/release/master?label=release&style=flat-square&event=push">
  </a>


  <a href="https://github.com/lrstanley/vault-unseal/actions?query=workflow%3Atest+event%3Apush">
    <img alt="GitHub Workflow Status (test @ master)" src="https://img.shields.io/github/workflow/status/lrstanley/vault-unseal/test/master?label=test&style=flat-square&event=push">
  </a>

  <img alt="Code Coverage" src="https://img.shields.io/codecov/c/github/lrstanley/vault-unseal/master?style=flat-square">

  <a href="https://pkg.go.dev/github.com/lrstanley/vault-unseal">
    <img alt="Go Documentation" src="https://pkg.go.dev/badge/github.com/lrstanley/vault-unseal?style=flat-square">
  </a>
  <a href="https://goreportcard.com/report/github.com/lrstanley/vault-unseal">
    <img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/lrstanley/vault-unseal?style=flat-square">
  </a>
  <img alt="Bug reports" src="https://img.shields.io/github/issues/lrstanley/vault-unseal/bug?label=issues&style=flat-square">
  <img alt="Feature requests" src="https://img.shields.io/github/issues/lrstanley/vault-unseal/enhancement?label=feature%20requests&style=flat-square">
  <a href="https://github.com/lrstanley/vault-unseal/pulls">
    <img alt="Open Pull Requests" src="https://img.shields.io/github/issues-pr/lrstanley/vault-unseal?style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/releases">
    <img alt="Latest Semver Release" src="https://img.shields.io/github/v/release/lrstanley/vault-unseal?style=flat-square">
    <img alt="Latest Release Date" src="https://img.shields.io/github/release-date/lrstanley/vault-unseal?style=flat-square">
  </a>
  <img alt="Last commit" src="https://img.shields.io/github/last-commit/lrstanley/vault-unseal?style=flat-square">
  <a href="https://github.com/lrstanley/vault-unseal/discussions/new?category=q-a">
    <img alt="Ask a Question" src="https://img.shields.io/badge/discussions-ask_a_question!-green?style=flat-square">
  </a>
  <a href="https://liam.sh/chat"><img src="https://img.shields.io/badge/discord-bytecord-blue.svg?style=flat-square" alt="Discord Chat"></a>
</p>
<!-- template:end:header -->

<!-- template:begin:toc -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :link: Table of Contents

  - [Why](#grey_question-why)
  - [Solution](#heavy_check_mark-solution)
  - [Installation](#computer-installation)
    - [Container Images (ghcr)](#whale-container-images-ghcr)
    - [Source](#toolbox-source)
  - [Usage](#gear-usage)
  - [TODO](#todo)
  - [License](#balance_scale-license)
<!-- template:end:toc -->

## :grey_question: Why

Depending on your use-case for Vault, you may or may not have opted for Vault
Enterprise. If you have not, auto-unseal functionality for on-prem is currently
only in enterprise (for cloud, it is now in the OSS version). If what you are
storing in vault isn't sensitive enough to require human intervention, you may
want to roll your own unseal functionality. The problem with this is it is very
hard to do safely.

So, what do we need to solve? we want to auto-unseal a vault, by providing the
necessary unseal tokens when we find vault is sealed. We also want to make sure
we're sending notifications when this happens, so if vault was unsealed
unintentionally (not patching, upgrades, etc), possibly related to crashing or
malicious intent, a human can investigate at a later time (**not** 3am in the
morning).

## :heavy_check_mark: Solution

The goal for this project is to find the best way to unseal vault in a way that
doesn't compromise too much security (a good balance between security and ease of
use/uptime), without the requirement of Vault Enterprise, or having to move to a
cloud platform.

We do this by running multiple instances of vault-unseal (you could run one
on each node in the cluster). Each instance of vault-unseal is given a subset
of the unseal tokens. You want to give each node **just enough** tokens, that
when paired with another vault-unseal node, they can work together to unseal the
vault. What we want to avoid is giving a single vault-unseal instance enough
tokens to unseal (to prevent a compromise leading to enough tokens being exposed
that could unseal the vault). Let's use the following example:

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

## :computer: Installation

Check out the [releases](https://github.com/lrstanley/vault-unseal/releases)
page for prebuilt versions.

<!-- template:begin:ghcr -->
<!-- do not edit anything in this "template" block, its auto-generated -->
### :whale: Container Images (ghcr)

```console
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.2.0
```
<!-- template:end:ghcr -->

### :toolbox: Source

Note that you must have [Go](https://golang.org/doc/install) installed (latest is usually best).

    $ git clone https://github.com/lrstanley/vault-unseal.git && cd vault-unseal
    $ make
    $ ./vault-unseal --help

## :gear: Usage

The default configuration path is `/etc/vault-unseal.yaml` when using `deb`/`rpm`.
If you are not using these package formats, copy the example config file,
`example.vault-unseal.yaml`, to `vault-unseal.yaml`. Note, all fields can be provided
via environment variables (vault-unseal also supports `.env` files).

```
$ ./vault-unseal --help
Usage:
  vault-unseal [OPTIONS]

Application Options:
  -v, --version          Display the version of vault-unseal and exit
  -l, --log-path=PATH    Optional path to log output to
  -c, --config=PATH      Path to configuration file (default: ./vault-unseal.yaml)

Help Options:
  -h, --help             Show this help message
```

## TODO

 - [ ] add option to use vault token/another vault instance to obtain keys (e.g. as long the leader is online)?
 - [ ] memory obfuscating/removing from memory right after unseal?

<!-- template:begin:support -->
<!-- template:end:support -->

<!-- template:begin:contributing -->
<!-- template:end:contributing -->

<!-- template:begin:license -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :balance_scale: License

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

_Also located [here](LICENSE)_
<!-- template:end:license -->
