<!-- template:define:options
{
  "nodescription": true
}
-->
![logo](https://liam.sh/-/gh/svg/lrstanley/vault-unseal?bg=topography&icon=file-icons%3Ahashicorp&icon.height=65&icon.color=rgba%280%2C+184%2C+126%2C+1%29)

<!-- template:begin:header -->
<!-- do not edit anything in this "template" block, its auto-generated -->

<p align="center">
  <a href="https://github.com/lrstanley/vault-unseal/releases">
    <img title="Release Downloads" src="https://img.shields.io/github/downloads/lrstanley/vault-unseal/total?style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/tags">
    <img title="Latest Semver Tag" src="https://img.shields.io/github/v/tag/lrstanley/vault-unseal?style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/commits/master">
    <img title="Last commit" src="https://img.shields.io/github/last-commit/lrstanley/vault-unseal?style=flat-square">
  </a>




  <a href="https://github.com/lrstanley/vault-unseal/actions?query=workflow%3Atest+event%3Apush">
    <img title="GitHub Workflow Status (test @ master)" src="https://img.shields.io/github/actions/workflow/status/lrstanley/vault-unseal/test.yml?branch=master&label=test&style=flat-square">
  </a>

  <a href="https://codecov.io/gh/lrstanley/vault-unseal">
    <img title="Code Coverage" src="https://img.shields.io/codecov/c/github/lrstanley/vault-unseal/master?style=flat-square">
  </a>

  <a href="https://pkg.go.dev/github.com/lrstanley/vault-unseal">
    <img title="Go Documentation" src="https://pkg.go.dev/badge/github.com/lrstanley/vault-unseal?style=flat-square">
  </a>
  <a href="https://goreportcard.com/report/github.com/lrstanley/vault-unseal">
    <img title="Go Report Card" src="https://goreportcard.com/badge/github.com/lrstanley/vault-unseal?style=flat-square">
  </a>
</p>
<p align="center">
  <a href="https://github.com/lrstanley/vault-unseal/issues?q=is:open+is:issue+label:bug">
    <img title="Bug reports" src="https://img.shields.io/github/issues/lrstanley/vault-unseal/bug?label=issues&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/issues?q=is:open+is:issue+label:enhancement">
    <img title="Feature requests" src="https://img.shields.io/github/issues/lrstanley/vault-unseal/enhancement?label=feature%20requests&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/pulls">
    <img title="Open Pull Requests" src="https://img.shields.io/github/issues-pr/lrstanley/vault-unseal?label=prs&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/releases">
    <img title="Latest Semver Release" src="https://img.shields.io/github/v/release/lrstanley/vault-unseal?style=flat-square">
    <img title="Latest Release Date" src="https://img.shields.io/github/release-date/lrstanley/vault-unseal?label=date&style=flat-square">
  </a>
  <a href="https://github.com/lrstanley/vault-unseal/discussions/new?category=q-a">
    <img title="Ask a Question" src="https://img.shields.io/badge/support-ask_a_question!-blue?style=flat-square">
  </a>
  <a href="https://liam.sh/chat"><img src="https://img.shields.io/badge/discord-bytecord-blue.svg?style=flat-square" title="Discord Chat"></a>
</p>
<!-- template:end:header -->

<!-- template:begin:toc -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :link: Table of Contents

  - [❔ Why](#grey_question-why)
  - [Solution](#heavy_check_mark-solution)
  - [Installation](#computer-installation)
    - [Container Images (ghcr)](#whale-container-images-ghcr)
    - [Source](#toolbox-source)
  - [Usage](#gear-usage)
  - [TODO](#ballot_box_with_check-todo)
  - [Support &amp; Assistance](#raising_hand_man-support--assistance)
  - [Contributing](#handshake-contributing)
  - [License](#balance_scale-license)
<!-- template:end:toc -->

## :grey_question: Why

HashiCorp Vault provides a few options for auto-unsealing clusters:

- [Cloud KMS (AWS, Azure, GCP, and others)](https://developer.hashicorp.com/vault/docs/configuration/seal/awskms) (cloud only)
- [Hardware Security Modules with PKCS11](https://developer.hashicorp.com/vault/docs/configuration/seal/pkcs11) (enterprise only)
- [Transit Engine via Vault](https://developer.hashicorp.com/vault/docs/configuration/seal/transit) (requires another vault cluster)
- [Potentially others](https://developer.hashicorp.com/vault/docs/configuration/seal)

However, depending on your deployment conditions and use-cases of Vault, some of
the above may not be feasible (cost, network connectivity, complexity). This may
lead you to want to roll your own unseal functionality, however, it's not easy to
do in a relatively secure manner.

So, what do we need to solve? We want to auto-unseal a vault cluster, by providing
the necessary unseal tokens when we find vault is sealed. We also want to make sure
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

![vault-unseal example diagram](https://cdn.liam.sh/share/2022/08/I8Qc1RCBMd.png)

Explained further:

- `cluster-1` consists of 3 nodes:
  - `node-1`
  - `node-2`
  - `node-3`
- `cluster-1` is configured with 5 unseal tokens (tokens `A`, `B`, `C`, `D`, `E`), but
     only 3 are required to unseal a given vault node.
- given there are 3 nodes, 3 tokens being required:
  - vault-unseal on `node-1` gets tokens `A` and `B`.
  - vault-unseal on `node-2` gets tokens `B` and `C`.
  - vault-unseal on `node-3` gets tokens `A` and `C`.

With the above configuration:

- Given each vault-unseal node, each node has two tokens.
- Given the tokens provided to vault-unseal, each token (`A`, `B`, and `C`), there
   are two instances of that token across nodes in the cluster.
- If `node-1` is completely hard-offline, nodes `node-2` and `node-3` should have
   all three tokens, so if the other two nodes reboot, as long as vault-unseal starts
   up on those nodes, vault-unseal will be able to unseal both.
- If `node-2` becomes compromised, and the tokens are read from the config
   file (note: vault-unseal **will not start** if the permissions on the file aren't
   `600`), this will not be enough tokens to unseal the vault.
- vault-unseal runs as root, with root permissions.

## :computer: Installation

Check out the [releases](https://github.com/lrstanley/vault-unseal/releases)
page for prebuilt versions.

<!-- template:begin:ghcr -->
<!-- do not edit anything in this "template" block, its auto-generated -->
### :whale: Container Images (ghcr)

```console
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:master
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.3.0
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:latest
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.2.4
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.2.3
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.2.2
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.2.1
$ docker run -it --rm ghcr.io/lrstanley/vault-unseal:0.2.0
```
<!-- template:end:ghcr -->

### :toolbox: Source

Note that you must have [Go](https://golang.org/doc/install) installed (latest is usually best).

    git clone https://github.com/lrstanley/vault-unseal.git && cd vault-unseal
    make
    ./vault-unseal --help

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

## :ballot_box_with_check: TODO

- [ ] add option to use vault token/another vault instance to obtain keys (e.g. as long the leader is online)?
- [ ] memory obfuscating/removing from memory right after unseal?

<!-- template:begin:support -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :raising_hand_man: Support & Assistance

* :heart: Please review the [Code of Conduct](.github/CODE_OF_CONDUCT.md) for
     guidelines on ensuring everyone has the best experience interacting with
     the community.
* :raising_hand_man: Take a look at the [support](.github/SUPPORT.md) document on
     guidelines for tips on how to ask the right questions.
* :lady_beetle: For all features/bugs/issues/questions/etc, [head over here](https://github.com/lrstanley/vault-unseal/issues/new/choose).
<!-- template:end:support -->

<!-- template:begin:contributing -->
<!-- do not edit anything in this "template" block, its auto-generated -->
## :handshake: Contributing

* :heart: Please review the [Code of Conduct](.github/CODE_OF_CONDUCT.md) for guidelines
     on ensuring everyone has the best experience interacting with the
    community.
* :clipboard: Please review the [contributing](.github/CONTRIBUTING.md) doc for submitting
     issues/a guide on submitting pull requests and helping out.
* :old_key: For anything security related, please review this repositories [security policy](https://github.com/lrstanley/vault-unseal/security/policy).
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
