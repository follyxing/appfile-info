# appfile-info
ipa and apk parser written in golang, aims to extract app information

[![Build Status](https://travis-ci.org/follyxing/appfile-info.svg?branch=master)](https://travis-ci.org/follyxing/appfile-info)


# APPFILE INFO

```go

  	//common
	Name                     string
	BundleId                 string
	Version                  string
	Build                    string
	Icon                     image.Image
	Size                     int64
	
	//apk file only
	ApkDebug                 bool
	
	//ipa file only
	IosPlatform              []string
	IosSigningType           string //development, ad-hoc, enterprise, app-store
	IosSigningExpirationDate string
	IosProvisionedDevices    []string
	
```



## INSTALL
	$ go get github.com/follyxing/appfile-info
  
## USAGE
```go
package main

import (
	"fmt"
	"github.com/follyxing/appfile-info"
)

func main() {
	apk, _ := ipapk.NewAppParser("test.apk")
	fmt.Println(apk)
}
```

# Thanks
fork from :
 https://github.com/phinexdaz/ipapk


