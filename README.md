# Wireplus: Wire Supercharged

Wireplus is a wire command superchaged with graphviz vizualisation.

The `wireplus` CLI includes an extra command `wireplus graph` which visualizes the dependency graph
in online Graphviz editor available at [https://edotor.net/](https://edotor.net/)

![wireplus demo](https://user-images.githubusercontent.com/28210288/268586107-58cae342-a579-4f38-ba1e-47612572adab.png)

*Demo graph generated from [https://github.com/google/go-cloud](https://github.com/google/go-cloud)

## Installing

Install wireplus by running:

```shell
go install github.com/taichimaeda/wireplus/cmd/wireplus@latest
```

and ensuring that `$GOPATH/bin` is added to your `$PATH`.

## Usage

```shell
wireplus graph github.com/google/path/to/package initializeApplication
```

or

```shell
cd /path/to/package
wireplus graph . initializeApplication
```
