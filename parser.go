package appfile

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/follyxing/go-plist"
	"github.com/andrianbdn/iospng"
	"github.com/fullsailor/pkcs7"
	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
)

var (
	reInfoPlist = regexp.MustCompile(`Payload/[^/]+/Info\.plist`)
	ErrNoIcon   = errors.New("icon not found")
)

const (
	iosExt     = ".ipa"
	androidExt = ".apk"
)

type AppInfo struct {
	Name                     string
	BundleId                 string
	Version                  string
	Build                    string
	Icon                     image.Image
	Size                     int64
	ApkDebug                 bool
	IosPlatform              []string
	IosSigningType           string
	IosSigningExpirationDate string
	IosProvisionedDevices    []string
}

type androidManifest struct {
	Package     string             `xml:"package,attr"`
	VersionName string             `xml:"versionName,attr"`
	VersionCode string             `xml:"versionCode,attr"`
	Application androidApplication `xml:"application"`
}
type iosProfile struct {
	Platform             []string               `plist:"Platform"`
	ProvisionedDevices   []string               `plist:"ProvisionedDevices"`
	ProvisionsAllDevices bool                   `plist:"ProvisionsAllDevices"`
	ExpirationDate       time.Time              `plist:"ExpirationDate"`
	Entitlements         iosProfileEntitlements `plist:"Entitlements"`
}

type iosProfileEntitlements struct {
	GetTaskAllow          bool   `plist:"get-task-allow"`
	BetaReportsActive     bool   `plist:"beta-reports-active"`
	ApplicationIdentifier string `plist:"application-identifier"`
}

type androidApplication struct {
	Debuggable string `xml:"debuggable,attr"`
}
type iosPlist struct {
	CFBundleName         string `plist:"CFBundleName"`
	CFBundleDisplayName  string `plist:"CFBundleDisplayName"`
	CFBundleVersion      string `plist:"CFBundleVersion"`
	CFBundleShortVersion string `plist:"CFBundleShortVersionString"`
	CFBundleIdentifier   string `plist:"CFBundleIdentifier"`
}

func NewAppParser(name string) (*AppInfo, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, err
	}

	var xmlFile, plistFile, iosIconFile, profileFile *zip.File
	for _, f := range reader.File {
		switch {
		case f.Name == "AndroidManifest.xml":
			xmlFile = f
		case reInfoPlist.MatchString(f.Name):
			plistFile = f
		case strings.Contains(f.Name, "AppIcon60x60"):
			iosIconFile = f
		case strings.Contains(f.Name, "embedded.mobileprovision"):
			profileFile = f
		}
	}

	ext := filepath.Ext(stat.Name())

	if ext == androidExt {
		info, err := parseApkFile(xmlFile)
		icon, label, err := parseApkIconAndLabel(name)
		info.Name = label
		info.Icon = icon
		info.Size = stat.Size()
		return info, err
	}

	if ext == iosExt {
		info, err := parseIpaFile(plistFile)
		profileInfo, err := parseIpaProfile(profileFile)
		if err != nil {
			return nil, err
		}
		icon, err := parseIpaIcon(iosIconFile)
		info.Icon = icon
		info.Size = stat.Size()
		info.IosPlatform = profileInfo.IosPlatform
		info.IosSigningType = profileInfo.IosSigningType
		info.IosSigningExpirationDate = profileInfo.IosSigningExpirationDate
		info.IosProvisionedDevices = profileInfo.IosProvisionedDevices
		return info, err
	}

	return nil, errors.New("unknown platform")
}

func parseAndroidManifest(xmlFile *zip.File) (*androidManifest, error) {
	rc, err := xmlFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	buf, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	xmlContent, err := androidbinary.NewXMLFile(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}

	manifest := new(androidManifest)
	decoder := xml.NewDecoder(xmlContent.Reader())
	if err := decoder.Decode(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func parseApkFile(xmlFile *zip.File) (*AppInfo, error) {
	if xmlFile == nil {
		return nil, errors.New("AndroidManifest.xml not found")
	}

	manifest, err := parseAndroidManifest(xmlFile)
	if err != nil {
		return nil, err
	}

	info := new(AppInfo)
	info.BundleId = manifest.Package
	info.Version = manifest.VersionName
	info.Build = manifest.VersionCode
	info.ApkDebug = manifest.Application.Debuggable == "true"

	return info, nil
}

func parseApkIconAndLabel(name string) (image.Image, string, error) {
	pkg, err := apk.OpenFile(name)
	if err != nil {
		return nil, "", err
	}
	defer pkg.Close()

	icon, _ := pkg.Icon(&androidbinary.ResTableConfig{
		Density: 720,
	})
	if icon == nil {
		return nil, "", ErrNoIcon
	}

	label, _ := pkg.Label(nil)

	return icon, label, nil
}

func parseIpaFile(plistFile *zip.File) (*AppInfo, error) {
	if plistFile == nil {
		return nil, errors.New("info.plist not found")
	}

	rc, err := plistFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	buf, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	p := new(iosPlist)
	decoder := plist.NewDecoder(bytes.NewReader(buf))
	if err := decoder.Decode(p); err != nil {
		return nil, err
	}

	info := new(AppInfo)
	if p.CFBundleDisplayName == "" {
		info.Name = p.CFBundleName
	} else {
		info.Name = p.CFBundleDisplayName
	}
	info.BundleId = p.CFBundleIdentifier
	info.Version = p.CFBundleShortVersion
	info.Build = p.CFBundleVersion

	return info, nil
}

func parseIpaIcon(iconFile *zip.File) (image.Image, error) {
	if iconFile == nil {
		return nil, ErrNoIcon
	}

	rc, err := iconFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var w bytes.Buffer
	iospng.PngRevertOptimization(rc, &w)

	return png.Decode(bytes.NewReader(w.Bytes()))
}

func parseIpaProfile(porfileFile *zip.File) (*AppInfo, error) {
	//# if ProvisionedDevices: !nil & "get-task-allow": true -> development
	//# if ProvisionedDevices: !nil & "get-task-allow": false -> ad-hoc
	//# if ProvisionedDevices: nil & "ProvisionsAllDevices": "true" -> enterprise
	//# if ProvisionedDevices: nil & ProvisionsAllDevices: nil -> app-store
	if porfileFile == nil {
		return nil, errors.New("profile not found")
	}

	rc, err := porfileFile.Open()
	if err != nil {
		return nil, errors.New("profile not found")
	}
	defer rc.Close()
	profileData, err := loadPKCS7Content(rc)
	if err != nil {
		log.Printf(err.Error())
	}
	decoder := plist.NewDecoder(bytes.NewReader(profileData))
	profile := new(iosProfile)
	if err := decoder.Decode(profile); err != nil {
		log.Printf(err.Error())
		return nil, err
	}

	provisionedDevices := profile.ProvisionedDevices
	ProvisionsAllDevices := profile.ProvisionsAllDevices
	getTaskAllow := profile.Entitlements.GetTaskAllow

	var signing string
	if provisionedDevices != nil {
		if getTaskAllow {
			signing = "development"
		} else {
			signing = "ad-hoc"
		}
	} else {
		if ProvisionsAllDevices {
			signing = "enterprise"
		} else {
			signing = "app-store"
		}
	}
	appInfo := AppInfo{}
	appInfo.IosPlatform = profile.Platform
	appInfo.IosProvisionedDevices = profile.ProvisionedDevices
	appInfo.IosSigningType = signing
	appInfo.IosSigningExpirationDate = strconv.FormatInt(profile.ExpirationDate.Unix(), 10)
	return &appInfo, nil

}

func loadPKCS7Content(r io.Reader) ([]byte, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read pkcs7 data: %s", err)
	}
	msg, err := pkcs7.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pkcs7: %s", err)
	}
	if err := msg.Verify(); err != nil {
		return nil, fmt.Errorf("failed to verify: %s", err)
	}
	return msg.Content, nil
}
