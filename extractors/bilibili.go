package extractors

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	//b2Regex     = regexp.MustCompile("window.__playinfo__=({.*?})<")
	//b2InfoRegex = regexp.MustCompile("window.__INITIAL_STATE__=({.*?});")
	b2AIDRegex = regexp.MustCompile("av(\\d+)")
	b2BIDRegex = regexp.MustCompile("(BV[A-Za-z0-9]+)")
)

type Bili struct {
	API
	cookie    string
	Title     string
	Meta      string
	VideoInfo b2VideoInfo
	DownInfo  b2PlayInfo
}
type b2VideoWarp struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    b2VideoInfo `json:"data"`
}

type b2VideoInfo struct {
	AvID int    `json:"aid"`
	BvID string `json:"bvid"`
	Tags []struct {
		ID   int    `json:"tag_id"`
		Name string `json:"tag_name"`
	} `json:"tags"`
	CTime  int64  `json:"pubdate"`
	Title  string `json:"title"`
	Pic    string `json:"pic"`
	UpData struct {
		Mid    int    `json:"mid"`
		Name   string `json:"name"`
		Fans   int    `json:"fans"`
		Friend int    `json:"friend"`
	} `json:"owner"`
	StatData struct {
		Dms   int `json:"danmaku"`
		Fav   int `json:"favorite"`
		Coin  int `json:"coin"`
		Lis   int `json:"like"`
		Reply int `json:"reply"`
		Share int `json:"share"`
		View  int `json:"view"`
	} `json:"stat"`
	Pages []struct {
		Cid      int    `json:"cid"`
		Page     int    `json:"page"`
		Part     string `json:"part"`
		Duration int    `json:"duration"`
	} `json:"pages"`
}

type b2PlayInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RateText []string `json:"accept_description"`
		RateInt  []int    `json:"accept_quality"`
		DashData struct {
			Duration int `json:"duration"`
			Audio    []struct {
				ID      int    `json:"id"`
				BaseUrl string `json:"base_url"`
				//BackupUrl string `json:"backup_url"`
				Bandwidth int    `json:"bandwidth"`
				Codecs    string `json:"codecs"`
			} `json:"audio"`
			Video []struct {
				ID      int    `json:"id"`
				Height  int    `json:"height"`
				Width   int    `json:"width"`
				BaseUrl string `json:"base_url"`
				//BackupUrl string `json:"backup_url"`
				Bandwidth int    `json:"bandwidth"`
				Codecs    string `json:"codecs"`
			} `json:"video"`
		} `json:"dash"`
		DurlData []struct {
			Duration int    `json:"duration"`
			URL      string `json:"url"`
		} `json:"durl"`
		Length  int `json:"timelength"`
		Quality int `json:"quality"`
	} `json:"data"`
}

func (l *Bili) GetSelf() string {
	return "Bilibili"
}

func (l *Bili) SetCookie(s string) {
	l.cookie = s
}

func (l *Bili) Download(band string, tn chan int) (string, error) {
	defer close(tn)
	found := false
	var videoURL string
	var audioURL string

	if len(l.DownInfo.Data.DashData.Video) != 0 {
		for _, o := range l.DownInfo.Data.DashData.Video {
			if strconv.Itoa(o.Bandwidth) == band {
				found = true
				audioURL = o.BaseUrl
				videoURL = l.DownInfo.Data.DashData.Audio[0].BaseUrl
			}
		}
		if !found {
			return "", fmt.Errorf("bandwidth unknown")
		}
	} else {
		for c, v := range l.DownInfo.Data.RateInt {
			if v == l.DownInfo.Data.Quality {
				found = true
				videoURL = l.DownInfo.Data.DurlData[c].URL
				break
			}
		}
	}

	log.Println(audioURL, videoURL, tn)
	audioFile := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".m4v")
	videoFile := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".m4v")

	ck := baseHeader
	if l.cookie != "" {
		ck["cookie"] = l.cookie
	}
	ck["referer"] = "https://www.bilibili.com/"
	err := dlHandler(videoFile, videoURL, ck, tn)
	if err != nil {
		log.Println(err)
		_ = os.Remove(videoFile)
		return "", err
	}

	if audioURL != "" {
		err := dlHandler(audioFile, audioURL, ck, tn)
		if err != nil {
			_ = os.Remove(audioFile)
			return "", err
		}
	}

	fin := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".mp4")
	var cmd *exec.Cmd
	if audioURL != "" {
		cmd = exec.Command("ffmpeg", "-i", videoFile, "-i", audioFile, "-c", "copy", fin)
	} else {
		cmd = exec.Command("ffmpeg", "-i", videoFile, "-c", "copy", fin)
	}
	_ = cmd.Run()
	return fin, nil
}

func (l *Bili) GetInfo(link string) (string, error) {

	if strings.Contains(link, "b23.tv") {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		res, err := client.Get(link)
		if err != nil {
			return "", fmt.Errorf("shortlink convert error")
		}

		link = res.Header.Get("Location")
	}
	// get id
	if p := b2AIDRegex.FindStringSubmatch(link); len(p) > 1 {
		link = "https://api.bilibili.com/x/web-interface/view?aid=" + p[1]
	} else if q := b2BIDRegex.FindStringSubmatch(link); len(q) > 1 {
		link = "https://api.bilibili.com/x/web-interface/view?bvid=" + q[1]
	} else {
		return "", fmt.Errorf("unknown link")
	}

	ck := baseHeader
	if l.cookie != "" {
		ck["cookie"] = l.cookie
	}
	//rsl, err := json.Marshal(ck)
	//if err != nil {
	//	return "", err
	//}

	for _, r := range []string{cfRelay, cnRelay, hkRelay} {
		param, err := json.Marshal(map[string]interface{}{
			"url":     link,
			"method":  "get",
			"headers": ck,
		})

		var body string
		if l.cookie != "" && r == cfRelay {
			body, err = query(usRelay, param)
		} else {
			body, err = query(r, param)
		}
		if err != nil {
			return "", fmt.Errorf("invalid response: %s", err)
		}
		var parsedResp1 b2VideoWarp
		if err := json.Unmarshal([]byte(body), &parsedResp1); err != nil {
			return "", err
		}

		if parsedResp1.Code == -404 && r != hkRelay {
			continue
		}

		if parsedResp1.Code == 0 {
			l.VideoInfo = parsedResp1.Data
			break
		} else {
			return "", fmt.Errorf("invalid response: %s", parsedResp1.Message)
		}
	}

	return l.GenMsgAndBts(), nil
}

func (l *Bili) GetDownInfo(p int) ([]string, []int, error) {
	if p > len(l.VideoInfo.Pages) {
		return nil, nil, fmt.Errorf("invalid count")
	}

	link := fmt.Sprintf("https://api.bilibili.com/x/player/playurl?bvid=%s&cid=%d&qn=120&fourk=1&fnval=16",
		l.VideoInfo.BvID, l.VideoInfo.Pages[p].Cid)

	ck := baseHeader
	if l.cookie != "" {
		ck["cookie"] = l.cookie
	}
	//rsl, err := json.Marshal(ck)
	//if err != nil {
	//	return nil, nil, err
	//}

	for _, r := range []string{cfRelay, cnRelay, hkRelay} {
		param, err := json.Marshal(map[string]interface{}{
			"url":     link,
			"method":  "get",
			"headers": ck,
		})
		var body string
		if l.cookie != "" && r == cfRelay {
			body, err = query(usRelay, param)
		} else {
			body, err = query(r, param)
		}
		if err != nil {
			log.Println(err)
			return nil, nil, fmt.Errorf("request failed, downinfo query")
		}

		var parsedResp2 b2PlayInfo
		if err := json.Unmarshal([]byte(body), &parsedResp2); err != nil {
			return nil, nil, err
		}

		if parsedResp2.Code == -404 && r != hkRelay {
			continue
		}

		if parsedResp2.Code == 0 {
			l.DownInfo = parsedResp2
			break
		} else {
			return nil, nil, fmt.Errorf("invalid response: %s", parsedResp2.Message)
		}
	}

	return l.GenDownMsg()
}

func (l *Bili) GetDuring() int32 {
	return int32(l.DownInfo.Data.DashData.Duration)
}

func (l *Bili) GetHeight(band string) int32 {
	for _, o := range l.DownInfo.Data.DashData.Video {
		if strconv.Itoa(o.Bandwidth) == band {
			return int32(o.Height)
		}
	}
	return 0
}

func (l *Bili) GetWidth(band string) int32 {
	for _, o := range l.DownInfo.Data.DashData.Video {
		if strconv.Itoa(o.Bandwidth) == band {
			return int32(o.Width)
		}
	}
	return 0
}

func (l *Bili) GetSubTitle(p int) string {
	return l.VideoInfo.Pages[p].Part
}

func (l *Bili) GetTitle(p int) string {
	if l.GetCount() > 1 {
		return l.VideoInfo.Title + " - " + l.VideoInfo.Pages[p].Part
	} else {
		return l.VideoInfo.Title
	}
}

func (l *Bili) GetMeta() string {
	return l.Meta
}

func (l *Bili) GetCount() int {
	return len(l.VideoInfo.Pages)
}

func (l *Bili) GetSteam(dest string) (*DownloadInfo, error) {
	info := new(DownloadInfo)
	for _, o := range l.DownInfo.Data.DashData.Video {
		if strconv.Itoa(o.Bandwidth) == dest {
			info.VideoURL = o.BaseUrl
			info.AudioURL = l.DownInfo.Data.DashData.Audio[0].BaseUrl
		}
	}
	return info, nil
}

func (l *Bili) GenMsgAndBts() string {
	var videoMeta string
	s1 := l.VideoInfo
	s3 := s1.StatData

	t2 := time.Unix(s1.CTime, 0).String()
	t3 := fmt.Sprintf("%s(id: %d)", s1.UpData.Name, s1.UpData.Mid)
	videoMeta = fmt.Sprintf("\n\nAvID: %d／BvID: %s\n发布: %s\n作者: %s\n\n评论 %d／播放 %d／弹幕 %d\n收藏 %d／点赞 %d／分享 %d\n\nCover: %s",
		s1.AvID, s1.BvID, t2, t3, s3.Reply, s3.View, s3.Dms, s3.Fav, s3.Lis, s3.Share, s1.Pic)

	l.Meta = videoMeta
	return l.GetTitle(0) + videoMeta
}

func (l *Bili) GenDownMsg() ([]string, []int, error) {
	s2 := l.DownInfo

	var videoRate []string
	var rateInt []int

	if len(s2.Data.DashData.Video) == 0 {
		// Durl mode
		if len(s2.Data.DurlData) == 0 {
			return nil, nil, fmt.Errorf("视频信息错误")
		}
		label := "unknown"
		for c, v := range s2.Data.RateInt {
			if v == s2.Data.Quality {
				label = s2.Data.RateText[c]
				break
			}
		}
		videoRate = append(videoRate, fmt.Sprintf("%s - %d", label, s2.Data.Quality))
		rateInt = append(rateInt, s2.Data.Quality)
	} else {
		for _, v := range s2.Data.DashData.Video {
			label := "unknown"
			for c, p := range s2.Data.RateInt {
				if v.ID == p {
					label = s2.Data.RateText[c]
					break
				}
			}
			videoRate = append(videoRate, fmt.Sprintf("%s - %dKbps - %s", label, v.Bandwidth/1000, v.Codecs))
			rateInt = append(rateInt, v.Bandwidth)
		}
	}

	return videoRate, rateInt, nil
}
