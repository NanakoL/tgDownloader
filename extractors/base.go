package extractors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	nested "github.com/antonfisher/nested-logrus-formatter"
	logLib "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	log = &logLib.Logger{
		Out:          os.Stderr,
		Formatter:    new(nested.Formatter),
		Hooks:        make(logLib.LevelHooks),
		Level:        logLib.InfoLevel,
		ExitFunc:     os.Exit,
		ReportCaller: false,
	}
	baseHeader = map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 " +
			"(KHTML, like Gecko) Chrome/63.0.3239.84 Safari/537.36",
		"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}
	acFunRegex = regexp.MustCompile("https?://[^.]+?.acfun.cn/\\D+?/\\D\\D(\\d+)")
	b2vRegex   = regexp.MustCompile("([^.]+?.bilibili.com/\\D+?/\\D\\D(.*)|b23.tv/[A-Za-z0-9]+)")
	ytRegex    = regexp.MustCompile("(?:youtube\\.com/(?:[^/]+/.+/|(?:v|e(?:mbed)?)/|.*[?&]v=)|youtu\\.be/)([^\"&?/\\s]{11})")
)

type API interface {
	SetCookie(string)

	GetMeta() string
	GetThumb() string
	GetCodec(string) string
	GetTitle(int) string
	GetSubTitle(int) string

	GetCount() int
	GetDuring() int32
	GetWidth(string) int32
	GetHeight(string) int32

	GetInfo(string) (string, error)
	GetDownInfo(int) ([]string, []int, error)
	Download(context.Context, string, chan int) (string, error)
}

type baseResp struct {
	Status int    `json:"status"`
	Result string `json:"result"`
}

func dlMonitor(ctx context.Context, cancel context.CancelFunc, c *writeCounter) {
	tick := time.Tick(15 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			total := c.Total + 1
			dur := time.Now().Unix() - c.ts + 1
			speed := int64(total) / dur
			log.Println(ByteCountIEC(int64(c.Total)), strconv.Itoa(int(dur))+"s", ByteCountIEC(speed)+"/s")
			if speed < 10240 && dur > 1 {
				cancel()
			}
		}
	}
}

func dlHandler(ctx context.Context, file, dest string, headers map[string]string, tn chan int) error {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	counter := &writeCounter{c: tn}
	total := int64(0)
	for i := 0; i < 30; i++ {
		headers["Range"] = fmt.Sprintf("bytes=%d-", total)
		params, err := json.Marshal(map[string]interface{}{
			"link":    dest,
			"headers": headers,
		})
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			_ = os.Remove(file)
			return fmt.Errorf("context cancelled (dlHandler.root)")
		default:
			dlCtx, cancel := context.WithCancel(ctx)
			go dlMonitor(dlCtx, cancel, counter)
			log.Println(string(params))
			n, err := partialDownload(dlCtx, params, counter, f)
			cancel()
			total += n
			if err != nil && (i < 9 || errors.Is(err, context.Canceled)) {
				if strings.Contains(err.Error(), "redirect") {
					newDest := strings.Replace(err.Error(), "redirect: ", "", -1)
					if _, err := url.Parse(newDest); err == nil {
						dest = newDest
					}
				}
				if err == httpErrForbidden {
					return err
				}
				log.Println("download error:", err)
				time.Sleep(time.Second)
				continue
			}
			if err != nil {
				_ = f.Close()
				_ = os.Remove(file)
				return err
			}
		}
		break
	}
	return nil
}

var httpErrForbidden = fmt.Errorf("403 content forbidden")

func partialDownload(ctx context.Context, params []byte, counter *writeCounter, f *os.File) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", dlRelay, bytes.NewReader(params))
	if err != nil {
		return 0, err
	}
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == 403 {
		return 0, httpErrForbidden
	}
	if resp.StatusCode == 302 && strings.Contains(string(params), "googlevideo") {
		return 0, fmt.Errorf("redirect: %s", resp.Header.Get("Location"))
	}
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return 0, fmt.Errorf("invalid response: %d, %s", resp.StatusCode, resp.Status)
	}
	n, err := io.Copy(f, io.TeeReader(resp.Body, counter))
	if err != nil {
		return n, err
	}
	_ = resp.Body.Close()
	return n, nil
}

func GetURL(link string) (string, error) {
	param, err := json.Marshal(map[string]interface{}{
		"url":     link,
		"method":  "get",
		"headers": baseHeader,
	})
	if err != nil {
		return "", err
	}
	return query(cfRelay, param)
}

func query(relay string, param []byte) (string, error) {

	log.Println(relay, string(param))
	req2, err := http.Post(relay, "text/plain", bytes.NewReader(param))
	if err != nil {
		return "", fmt.Errorf("network error")
	}
	body2, err := ioutil.ReadAll(req2.Body)
	if err != nil {
		return "", err
	}
	_ = req2.Body.Close()
	//log.Println(string(body2))

	var resp2 baseResp
	if err := json.Unmarshal(body2, &resp2); err != nil {
		return "", err
	}
	return resp2.Result, nil
}

//func queryDirect(link string) (string, error) {
//	req2, err := http.Get(link)
//	if err != nil {
//		return "", fmt.Errorf("network error")
//	}
//	body2, err := ioutil.ReadAll(req2.Body)
//	if err != nil {
//		return "", err
//	}
//	_ = req2.Body.Close()
//
//	return string(body2), nil
//}

func ResolveURL(u *url.URL, p string) string {
	if strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "http://") {
		return p
	}
	var baseURL string
	if strings.Index(p, "/") == 0 {
		baseURL = u.Scheme + "://" + u.Host
	} else {
		tU := u.String()
		baseURL = tU[0:strings.LastIndex(tU, "/")]
	}
	return baseURL + path.Join("/", p)
}

func HttpGet(ctx context.Context, link string) (io.ReadCloser, error) {
	mds, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	ck := deepCopy(baseHeader).(map[string]string)
	ck["Referer"] = mds.Scheme + "://" + mds.Host
	params, err := json.Marshal(map[string]interface{}{
		"link":    link,
		"headers": ck,
	})
	//log.Println(string(params))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", dlRelay, bytes.NewReader(params))
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return nil, fmt.Errorf("http error: status code %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func Resolver(url string) API {
	r := []byte(url)
	switch {
	case acFunRegex.Match(r):
		return new(AcFun)
	case b2vRegex.Match(r):
		return new(Bili)
	case ytRegex.Match(r):
		return new(Youtube)
	default:
		return nil
	}
}

type writeCounter struct {
	Total uint64
	ts    int64
	c     chan int
}

// https://gist.github.com/albulescu/e61979cc852e4ee8f49c
func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	if time.Now().Unix()-wc.ts > 1 {
		wc.c <- int(wc.Total)
		wc.Total = 0
		wc.ts = time.Now().Unix()
	}
	return n, nil
}

func ByteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}

func deepCopy(value interface{}) interface{} {
	if valueMap, ok := value.(map[string]interface{}); ok {
		newMap := make(map[string]interface{})
		for k, v := range valueMap {
			newMap[k] = deepCopy(v)
		}

		return newMap
	} else if valueSlice, ok := value.([]interface{}); ok {
		newSlice := make([]interface{}, len(valueSlice))
		for k, v := range valueSlice {
			newSlice[k] = deepCopy(v)
		}

		return newSlice
	}

	return value
}
