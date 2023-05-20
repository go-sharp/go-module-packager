# go-module-packager
A Go Module packager for use in an air gapped environment.

# Usefull Links 

- [Go Module Reference](https://go.dev/ref/mod)

## API 

```go
    apiMux.Handle("/search", apiHandler(s.serveAPISearch))
	apiMux.Handle("/packages", apiHandler(s.serveAPIPackages))
	apiMux.Handle("/importers/", apiHandler(s.serveAPIImporters))
	apiMux.Handle("/imports/", apiHandler(s.serveAPIImports))

```
	