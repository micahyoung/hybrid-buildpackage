# Windows-Linux hybrid buildpackage

## Usage
```
go run main.go -ref hybrid-buildpackage:latest
```
use `-publish` to write directly to a registry

```
# Linux daemon
pack build myapp --buildpack docker://hybrid-buildpackage --builder cnbs/sample-builder:alpine 

# Windows daemon
pack build myapp --buildpack docker://hybrid-buildpackage --builder cnbs/sample-builder:nanoserver-1809 
```
