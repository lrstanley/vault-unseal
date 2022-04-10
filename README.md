<!-- template:begin:header -->
<!-- template:end:header -->

<!-- template:begin:toc -->
<!-- template:end:toc -->

## Why

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

## Solution

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

## Installation

Check out the [releases](https://github.com/lrstanley/vault-unseal/releases)
page for prebuilt versions.

<!-- template:begin:ghcr -->
<!-- template:end:ghcr -->

### Source

Note that you must have [Go](https://golang.org/doc/install) installed (latest is usually best).

    $ git clone https://github.com/lrstanley/vault-unseal.git && cd vault-unseal
    $ make
    $ ./vault-unseal --help

## Usage

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
<!-- template:end:license -->
