# Wireviz: Wire Supercharged with Graphviz Vizualisation

Wireviz is a wire command superchaged with graphviz vizualisation.

The `wireviz` CLI includes an extra command `wireviz graph` which visualizes the dependency graph
in online Graphviz editor available at [https://edotor.net/](https://edotor.net/)

![wireviz demo](docs/images/demo.png)

*Demo graph generated from [https://github.com/google/go-cloud](https://github.com/google/go-cloud)

## Installing

Install Wireviz by running:

```shell
go install github.com/taichimaeda/wireviz/cmd/wireviz@latest
```

and ensuring that `$GOPATH/bin` is added to your `$PATH`.

## Usage

```shell
wireviz github.com/google/path/to/package initializeApplication
```

or

```shell
cd /path/to/package
wireviz . initializeApplication
```
