package extractors

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"time"
)

var (
	acRegex   = regexp.MustCompile("window.videoInfo\\s*=\\s*({.*?});")
	acIDRegex = regexp.MustCompile("ac\\d+")
)

type AcFun struct {
	API
	cookie    string
	acID      string
	Title     string
	Meta      string
	VideoInfo acVideoInfo
	DownInfo  acPlayInfo
}

type acVideoInfo struct {
	Title string `json:"title"`
	//STitle           string     `json:"showTitle"`
	TimeStamp  int64  `json:"createTimeMillis"`
	TimeStamp2 string `json:"updateTime"`
	Dms        string `json:"danmakuCountShow"`
	Pls        string `json:"viewCountShow"`
	Pls2       string `json:"playCountShow"`
	Cms        string `json:"commentCountShow"`
	Lis        string `json:"likeCountShow"`
	Shr        string `json:"shareCountShow"`
	Sto        string `json:"stowCountShow"`
	Bnn        string `json:"bananaCountShow"`
	User       struct {
		Fan  string `json:"fanCount"`
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"user"`
	Cover            string `json:"coverUrl"`
	Cover2           string `json:"image"`
	CurrentVideoInfo struct {
		Dur      int    `json:"durationMillis"`
		Vid      string `json:"id"`
		PlayJson string `json:"ksPlayJson"`
	} `json:"currentVideoInfo"`
	PlayList []struct {
		Title string `json:"title"`
		Vid   string `json:"id"`
	} `json:"videoList"`
	VideoID int `json:"videoId"`
}

type acPlayInfo struct {
	Config struct {
		Config []struct {
			Bandwidth int      `json:"bandwidth"`
			Height    int      `json:"height"`
			Width     int      `json:"width"`
			Codecs    string   `json:"codecs"`
			AliURL    []string `json:"backupUrl"`
			Label     string   `json:"qualityLabel"`
		} `json:"representation"`
	} `json:"adaptationSet"`
}

func (l *AcFun) GetSelf() string {
	return "AcFun"
}

func (l *AcFun) SetCookie(s string) {
	l.cookie = s
}

func (l *AcFun) Download(band string, tn chan int) (string, error) {

	defer close(tn)
	var dest string
	for _, o := range l.DownInfo.Config.Config {
		if strconv.Itoa(o.Bandwidth) == band {
			dest = o.AliURL[0]
		}
	}
	if dest == "" {
		return "", fmt.Errorf("internal error")
	}

	src, err := ParseFromURL(dest)
	if err != nil {
		return "", err
	}
	file, err := ioutil.TempFile("", "")
	if err != nil {
		return "", err
	}

	for _, v := range src.Segments {
		var content []byte
		for {
			r := ResolveURL(src.URL, v.URI)
			log.Println(r)
			body, err := HttpGet(r)
			if err == nil && body != nil {
				content, err = ioutil.ReadAll(body)
				_ = body.Close()
				if err == nil {
					break
				}
			}
			time.Sleep(time.Second)
		}
		n, _ := file.Write(content)
		tn <- n
	}
	_ = file.Close()
	fin := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".mp4")
	cmd := exec.Command("ffmpeg", "-i", file.Name(), "-c", "copy", fin)
	_ = cmd.Run()
	_ = os.Remove(file.Name())
	return fin, nil
}

func (l *AcFun) GetInfo(link string) (string, error) {

	if r := acIDRegex.FindString(link); r != "" {
		l.acID = r
	} else {
		return "", fmt.Errorf("unknown link")
	}
	ck := baseHeader
	if l.cookie != "" {
		ck["cookie"] = l.cookie
	}
	//rsl, err := json.Marshal(ck)

	param, err := json.Marshal(map[string]interface{}{
		"url":     link,
		"method":  "get",
		"headers": ck,
	})

	var body string
	if l.cookie != "" {
		body, err = query(usRelay, param)
	} else {
		body, err = query(cfRelay, param)
	}

	if err != nil {
		return "", err
	}

	//log.Println(body)

	ext := acRegex.FindStringSubmatch(body)
	if len(ext) < 2 {
		return "", fmt.Errorf("invalid response")
	}

	var parsedResp1 acVideoInfo
	if err := json.Unmarshal([]byte(ext[1]), &parsedResp1); err != nil {
		return "", err
	}

	var parsedResp2 acPlayInfo
	//fmt.Println(parsedResp1.CurrentVideoInfo.PlayJson)
	if err := json.Unmarshal([]byte(parsedResp1.CurrentVideoInfo.PlayJson), &parsedResp2); err != nil {
		return "", err
	}
	l.VideoInfo = parsedResp1
	l.DownInfo = parsedResp2
	return l.GenMsgAndBts()
}

func (l *AcFun) GetMeta() string {
	return l.Meta
}

func (l *AcFun) GetHeight(band string) int32 {
	for _, o := range l.DownInfo.Config.Config {
		if strconv.Itoa(o.Bandwidth) == band {
			return int32(o.Height)
		}
	}
	return 0
}

func (l *AcFun) GetWidth(band string) int32 {
	for _, o := range l.DownInfo.Config.Config {
		if strconv.Itoa(o.Bandwidth) == band {
			return int32(o.Width)
		}
	}
	return 0
}

func (l *AcFun) GetCount() int {
	return len(l.VideoInfo.PlayList)
}

func (l *AcFun) GetDuring() int32 {
	return int32(l.VideoInfo.CurrentVideoInfo.Dur / 1000)
}

func (l *AcFun) GetSubTitle(p int) string {
	return l.VideoInfo.PlayList[p].Title
}

func (l *AcFun) GetTitle(p int) string {
	if l.GetCount() > 1 {
		return l.VideoInfo.Title + " - " + l.VideoInfo.PlayList[p].Title
	} else {
		return l.VideoInfo.Title
	}
}

func (l *AcFun) GenMsgAndBts() (string, error) {
	s1 := l.VideoInfo
	title := s1.Title
	if len(s1.PlayList) != 1 {
		for _, v := range s1.PlayList {
			if v.Vid == s1.CurrentVideoInfo.Vid {
				title = fmt.Sprintf("%s - %s", s1.Title, v.Title)
				break
			}
		}
	}
	t2 := time.Unix(s1.TimeStamp/1000, 0).String()
	t3 := fmt.Sprintf("%s(id: %s, 粉丝: %s)", s1.User.Name, s1.User.ID, s1.User.Fan)
	videoMeta := fmt.Sprintf("\n\nAcID: %s\n发布日期: %s\n作者: %s\n\n香蕉 %s／评论 %s\n播放 %s／弹幕 %s\n收藏 %s／喜欢 %s／分享 %s\n\nCover: %s",
		s1.CurrentVideoInfo.Vid, t2, t3, s1.Bnn, s1.Cms, s1.Pls, s1.Dms, s1.Sto, s1.Lis, s1.Shr, s1.Cover)

	l.Meta = videoMeta
	return title + videoMeta, nil
}

func (l *AcFun) GetDownInfo(p int) ([]string, []int, error) {
	if l.VideoInfo.CurrentVideoInfo.Vid != l.VideoInfo.PlayList[p].Vid {
		_, _ = l.GetInfo(fmt.Sprintf("https://www.acfun.cn/v/%s_%d", l.acID, p))
	}

	s2 := l.DownInfo
	var videoRate []string
	var rateInt []int

	for _, v := range s2.Config.Config {
		videoRate = append(videoRate, fmt.Sprintf("%s - %dKbps - %s", v.Label, v.Bandwidth/1000, v.Codecs))
		rateInt = append(rateInt, v.Bandwidth)
	}

	return videoRate, rateInt, nil
}
