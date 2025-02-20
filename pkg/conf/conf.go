package conf

import (
	"github.com/cloudreve/Cloudreve/v3/pkg/util"
	"github.com/go-ini/ini"
	"github.com/spf13/afero"
	"github.com/spf13/afero/rclonefs"
	"gopkg.in/go-playground/validator.v9"
	"path/filepath"
	"runtime"
	"strings"
)

// database 数据库
type database struct {
	Type        string
	User        string
	Password    string
	Host        string
	Name        string
	TablePrefix string
	DBFile      string
	Port        int
	Charset     string
}

// system 系统通用配置
type system struct {
	Mode          string `validate:"eq=master|eq=slave"`
	Listen        string `validate:"required"`
	Debug         bool
	SessionSecret string
	HashIDSalt    string
}

type ssl struct {
	CertPath string `validate:"omitempty,required"`
	KeyPath  string `validate:"omitempty,required"`
	Listen   string `validate:"required"`
}

type unix struct {
	Listen string
}

// slave 作为slave存储端配置
type slave struct {
	Secret          string `validate:"omitempty,gte=64"`
	CallbackTimeout int    `validate:"omitempty,gte=1"`
	SignatureTTL    int    `validate:"omitempty,gte=1"`
}

// captcha 验证码配置
type captcha struct {
	Height             int `validate:"gte=0"`
	Width              int `validate:"gte=0"`
	Mode               int `validate:"gte=0,lte=3"`
	ComplexOfNoiseText int `validate:"gte=0,lte=2"`
	ComplexOfNoiseDot  int `validate:"gte=0,lte=2"`
	IsShowHollowLine   bool
	IsShowNoiseDot     bool
	IsShowNoiseText    bool
	IsShowSlimeLine    bool
	IsShowSineLine     bool
	CaptchaLen         int `validate:"gt=0"`
}

// redis 配置
type redis struct {
	Network  string
	Server   string
	Password string
	DB       string
}

// 缩略图 配置
type thumb struct {
	MaxWidth   uint
	MaxHeight  uint
	FileSuffix string `validate:"min=1"`
}

// 跨域配置
type cors struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	ExposeHeaders    []string
}

// RClone配置
// Binds /mnt/ibm:ibm
type rclone struct {
	Config     string
	Binds     []string
}

var cfg *ini.File

const defaultConf = `[System]
Mode = master
Listen = :5212
SessionSecret = {SessionSecret}
HashIDSalt = {HashIDSalt}
`

// Init 初始化配置文件
func Init(path string) {
	var err error

	if path == "" || !util.Exists(path) {
		// 创建初始配置文件
		confContent := util.Replace(map[string]string{
			"{SessionSecret}": util.RandStringRunes(64),
			"{HashIDSalt}":    util.RandStringRunes(64),
		}, defaultConf)
		f, err := util.CreatNestedFile(path)
		if err != nil {
			util.Log().Panic("无法创建配置文件, %s", err)
		}

		// 写入配置文件
		_, err = f.WriteString(confContent)
		if err != nil {
			util.Log().Panic("无法写入配置文件, %s", err)
		}

		f.Close()
	}

	cfg, err = ini.Load(path)
	if err != nil {
		util.Log().Panic("无法解析配置文件 '%s': %s", path, err)
	}

	sections := map[string]interface{}{
		"Database":   DatabaseConfig,
		"System":     SystemConfig,
		"SSL":        SSLConfig,
		"UnixSocket": UnixConfig,
		"Captcha":    CaptchaConfig,
		"Redis":      RedisConfig,
		"Thumbnail":  ThumbConfig,
		"CORS":       CORSConfig,
		"RClone":     RCloneConfig,
		"Slave":      SlaveConfig,
	}
	for sectionName, sectionStruct := range sections {
		err = mapSection(sectionName, sectionStruct)
		if err != nil {
			util.Log().Panic("配置文件 %s 分区解析失败: %s", sectionName, err)
		}
	}

	// 重设log等级
	if !SystemConfig.Debug {
		util.Level = util.LevelInformational
		util.GloablLogger = nil
		util.Log()
	}

	initRCloneBind()
}

// mapSection 将配置文件的 Section 映射到结构体上
func mapSection(section string, confStruct interface{}) error {
	err := cfg.Section(section).MapTo(confStruct)
	if err != nil {
		return err
	}

	// 验证合法性
	validate := validator.New()
	err = validate.Struct(confStruct)
	if err != nil {
		return err
	}

	return nil
}


func initRCloneBind(){
	if runtime.GOOS != "linux"{
		util.Log().Warning("RClone Bind Unsupported OS %s until tested", runtime.GOOS)
		return
	}

	if RCloneConfig.Binds[0] == "UNSET"{
		return
	}

	if ok := util.Exists(RCloneConfig.Config); ok{
		_ = rclonefs.SetConfigPath(RCloneConfig.Config)
	}else{
		util.Log().Warning("未找到RClone配置文件: %s", RCloneConfig.Config)
		return
	}

	bindPoints := make(map[string]afero.Fs)
	bindPoints["/"] = afero.NewOsFs()

	for _, kv := range RCloneConfig.Binds{
		if bind := strings.SplitN(kv,":", 2); len(bind)==2{
			util.Log().Info("RClone绑定: '%s' -> '%s'", bind[1], bind[0])
			if target, err := filepath.Abs(bind[0]); err==nil{
				bindPoints[target] = rclonefs.NewRCloneFs(bind[1])
			}else{
				util.Log().Warning("绑定绝对路径出错: '%s'", bind[0])
			}
		}else{
			util.Log().Warning("RClone绑定不符合格式: %s", kv)
			return
		}
	}

	util.OS = rclonefs.NewBindPathFs(bindPoints)
}