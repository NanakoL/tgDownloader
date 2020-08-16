# tgDownloader

基于 NanakoL/Mtproto 的流媒体下载bot

## install

个人部署说明：

需要补充`./const.go`，文件的内容为：

./const.go
```
package main

const (
	appID         = int32()
	appHash       = ""
	token4        = "" // dc4
	token1        = "" // dc1
	token5        = "" // dc5
)

```

Note：此处有三个bot_token是为了多数据中心部署，如不需要可以减少token数量（需要同样更改`main.go`的相关内容）
