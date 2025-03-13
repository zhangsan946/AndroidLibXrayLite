# AndroidLibXrayLite

## Build requirements
* JDK
* Android SDK
* Go
* gomobile
    ```
    go install golang.org/x/mobile/cmd/gomobile@latest
    ```

## Build instructions
1. `git clone -b spaceship https://github.com/zhangsan946/AndroidLibXrayLite && cd AndroidLibXrayLite`
2. `gomobile init`
3. `go mod tidy -v`
4. `gomobile bind -v -androidapi 21 -trimpath -ldflags="-s -w -buildid=" ./`

## To update go mod at latest/specific commit

```
go get github.com/xtls/xray-core@latest
go get github.com/xtls/xray-core@98a72b6fb49b
```
