# Lilypad v2 🍃

This cloud is just someone else's computer.

![image](https://raw.githubusercontent.com/Lilypad-Tech/lilypad/main/assets/xkcd-the-cloud.png)

Lilypad enables users to run AI workloads easily in a decentralized GPU network where anyone can get paid to connect their compute nodes to the network and run jobs. Users have access to easy Stable Diffusion XL and cutting edge open source LLMs both on chain, from CLI and via [Lilypad AI Studio](https://lilypad.tech) on the web.

# Getting started

Welcome to the prerelease series of Lilypad v2.

## Aurora Testnet

The testnet has a base curency of ETH and you will also get LP to pay for jobs (and nodes to stake).

Metamask:

```
Network name: Lilypad v2 Aurora testnet
New RPC URL: http://testnet.lilypad.tech:8545
Chain ID: 1337
Currency symbol: ETH
Block explorer URL: (leave blank)
```

### Fund your wallet with ETH and LP

To obtain funds, go to [https://faucet.lilypad.tech](https://faucet.lilypad.tech).

The faucet will give you both ETH (to pay for gas) and LP (to stake and pay for jobs).

## Install CLI

Download the latest release of Lilypad for your platform. Both the amd64/x86_64 and arm64 variants of macOS and Linux are supported. (If you are on Apple Silicon, you'll want arm64). 

Nb: to check your version use ```which lilypad``` - if you're an old version run ```rm <path>``` to remove it, then install the newest version. You'll want to stay up-to-date to remain compatible with Lilypad as it evolves.

The commands below will automatically detect your computer's OS and processor architecture and download the correct Lilypad build for your machine.

```
# Detect your machine's architecture and set it as $OSARCH
OSARCH=$(uname -m | awk '{if ($0 ~ /arm64|aarch64/) print "arm64"; else if ($0 ~ /x86_64|amd64/) print "amd64"; else print "unsupported_arch"}') && export OSARCH
# Detect your operating system and set it as $OSNAME
OSNAME=$(uname -s | awk '{if ($1 == "Darwin") print "darwin"; else if ($1 == "Linux") print "linux"; else print "unsupported_os"}') && export OSNAME;
```
Then, download & install:
```
# Download the latest production build
curl -sSL -o lilypad https://github.com/lilypad-tech/lilypad/releases/download/v2.0.0-d63a7ff/lilypad-$OSNAME-$OSARCH
# Make Lilypad executable and install it
chmod +x lilypad
sudo mv lilypad /usr/local/bin/lilypad
```

You can also, at your option, choose to compile Lilypad using Go and install it that way on any machine that supports the Go toolchain.

## Run a job

```
export WEB3_PRIVATE_KEY=<your private key>
```
(or arrange for the key to be in your environment in a more secure way that doesn't get written to your shell history)

### Cows
```
lilypad run cowsay:v0.0.1 -i Message="moo"
```

### SDXL
```
lilypad run sdxl:v0.9-lilypad1 -i PromptEnv="PROMPT=beautiful view of iceland with a record player"
```

![image-42](https://github.com/lilypad-tech/lilypad/assets/264658/d48bb897-79a0-4f3a-b938-e85a8cfa3f0e)

## Run a node, earn LP
```
lilypad serve
```

systemd units & more details [here](https://github.com/lilypad-tech/lilypad/tree/main/ops)

## Available modules

Check the github releases page for each module or just use the git hash as the tag.

* [sdxl](https://github.com/lilypad-tech/lilypad-module-sdxl)
* [stable-diffusion](https://github.com/lilypad-tech/lilypad-module-stable-diffusion)
* [duckdb](https://github.com/lilypad-tech/lilypad-module-duckdb)
* [fastchat](https://github.com/lilypad-tech/lilypad-module-fastchat)
* [lora-inference](https://github.com/lilypad-tech/lilypad-module-lora-inference)
* [lora-training](https://github.com/lilypad-tech/lilypad-module-lora-training)
* [filecoin-data-prep](https://github.com/lilypad-tech/lilypad-module-filecoin-data-prep)
* [wasm](https://github.com/lilypad-tech/lilypad-module-wasm)
* [cowsay](https://github.com/lilypad-tech/lilypad-module-cowsay)

## Write a module

A module is just a Git repository with job information, usually associated with a Docker container.

Module versions are based on tags.

In your repository, create a file called `lilypad_module.json.tmpl`

See [cowsay](https://github.com/lilypad-tech/lilypad-module-cowsay) for example.

This is a JSON template with Go text/template style `{{.Message}}` sections which will be replaced by Lilypad with json encoded inputs to modules. You can also do fancy things with Go templates like setting defaults, see cowsay for example. 

While developing a module, you can use the git hash to test it.

Pass inputs as:
```
lilypad run github.com/username/repo:tag -i Message=moo
```

Inputs are a map of strings to strings.

**YOU MUST MAKE YOUR MODULE DETERMINISTIC**

Tips:
* Make the output reproducible, for example for the diffusers library, see [here](https://huggingface.co/docs/diffusers/using-diffusers/reproducibility)
* Strip timestamps and time measurements out of the output, including to stdout/stderr
* Don't read any sources of entropy (e.g. /dev/random)
* When referencing docker images, you MUST specify their sha256 hashes, as shown in this example

If your module is not deterministic, compute providers will not adopt it and blacklist your module

### Module-Next
We've got a new style of setting environment variables (and other useful templating) to allow for injection of user-provided variables without allowing unsafe text.

`"{{ if .Prompt }}{{ subst "PROMPT=%s" .Prompt }}{{ end }}",`

This example will insert the contents of Prompt (`-i Prompt="some prompt"`) as the environment variable $PROMPT.
