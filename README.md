# co

<p>
    <a href="docs/logo.svg"><img width="220" alt="co" src="docs/logo.svg"></a><br>
</p>


🚧 **Status:** actively under development and not stable. Interfaces, behavior, and the on-disk config may change without notice.

`co` is a coding assistant for the directory you start it in.

The handbook is at <https://kevinjom.github.io/co>. It is published from the latest stable release.

## Install

```
curl -fsSL https://raw.githubusercontent.com/kevinjom/co/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/kevinjom/co/main/install.sh | sh -s -- 0.1.0
```

To build from a checkout, Go 1.26.4 or newer:

```sh
git clone https://github.com/kevinjom/co.git
cd co
go build -o co ./cmd/co

# or run directly
go run ./cmd/co
```

The same toolchain installs the latest commit:

```sh
go install github.com/kevinjom/co/cmd/co@latest
```

## License

[MIT](LICENSE)
