# AndroidLibXrayLite

## Build requirements
* JDK
* Android SDK
* Go
* gomobile

## Build instructions
1. `git clone [repo] && cd AndroidLibXrayLite`
2. `gomobile init`
3. `go mod tidy -v`
4. `gomobile bind -v -androidapi 21 -ldflags="-s -w" ./`

## To update go mod at a specific commit

```
go get github.com/xtls/xray-core@98a72b6fb49b
``` 