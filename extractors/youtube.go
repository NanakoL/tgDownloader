package extractors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Youtube struct {
	API
	cookie    string
	Duration  time.Duration
	VideoInfo ytPlayerResponseData
	Formats   ytFormatList
}

// inherit from https://github.com/kkdai/youtube

type ytFormatList []Format

type ytPlayerResponseData struct {
	PlayabilityStatus struct {
		Status          string `json:"status"`
		Reason          string `json:"reason"`
		PlayableInEmbed bool   `json:"playableInEmbed"`
		ContextParams   string `json:"contextParams"`
	} `json:"playabilityStatus"`
	StreamingData struct {
		ExpiresInSeconds string   `json:"expiresInSeconds"`
		Formats          []Format `json:"formats"`
		AdaptiveFormats  []Format `json:"adaptiveFormats"`
	} `json:"streamingData"`
	Captions struct {
		PlayerCaptionsRenderer struct {
			BaseURL    string `json:"baseUrl"`
			Visibility string `json:"visibility"`
		} `json:"playerCaptionsRenderer"`
		PlayerCaptionsTracklistRenderer struct {
			CaptionTracks []struct {
				BaseURL string `json:"baseUrl"`
				Name    struct {
					SimpleText string `json:"simpleText"`
				} `json:"name"`
				VssID          string `json:"vssId"`
				LanguageCode   string `json:"languageCode"`
				Kind           string `json:"kind"`
				IsTranslatable bool   `json:"isTranslatable"`
			} `json:"captionTracks"`
			AudioTracks []struct {
				CaptionTrackIndices []int `json:"captionTrackIndices"`
			} `json:"audioTracks"`
			TranslationLanguages []struct {
				LanguageCode string `json:"languageCode"`
				LanguageName struct {
					SimpleText string `json:"simpleText"`
				} `json:"languageName"`
			} `json:"translationLanguages"`
			DefaultAudioTrackIndex int `json:"defaultAudioTrackIndex"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
	VideoDetails struct {
		VideoID          string `json:"videoId"`
		Title            string `json:"title"`
		LengthSeconds    string `json:"lengthSeconds"`
		ChannelID        string `json:"channelId"`
		IsOwnerViewing   bool   `json:"isOwnerViewing"`
		ShortDescription string `json:"shortDescription"`
		IsCrawlable      bool   `json:"isCrawlable"`
		Thumbnail        struct {
			Thumbnails []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"thumbnails"`
		} `json:"thumbnail"`
		AverageRating     float64 `json:"averageRating"`
		AllowRatings      bool    `json:"allowRatings"`
		ViewCount         string  `json:"viewCount"`
		Author            string  `json:"author"`
		IsPrivate         bool    `json:"isPrivate"`
		IsUnpluggedCorpus bool    `json:"isUnpluggedCorpus"`
		IsLiveContent     bool    `json:"isLiveContent"`
	} `json:"videoDetails"`
	PlayerConfig struct {
		AudioConfig struct {
			LoudnessDb              float64 `json:"loudnessDb"`
			PerceptualLoudnessDb    float64 `json:"perceptualLoudnessDb"`
			EnablePerFormatLoudness bool    `json:"enablePerFormatLoudness"`
		} `json:"audioConfig"`
		StreamSelectionConfig struct {
			MaxBitrate string `json:"maxBitrate"`
		} `json:"streamSelectionConfig"`
		MediaCommonConfig struct {
			DynamicReadaheadConfig struct {
				MaxReadAheadMediaTimeMs int `json:"maxReadAheadMediaTimeMs"`
				MinReadAheadMediaTimeMs int `json:"minReadAheadMediaTimeMs"`
				ReadAheadGrowthRateMs   int `json:"readAheadGrowthRateMs"`
			} `json:"dynamicReadaheadConfig"`
		} `json:"mediaCommonConfig"`
	} `json:"playerConfig"`
	Storyboards struct {
		PlayerStoryboardSpecRenderer struct {
			Spec string `json:"spec"`
		} `json:"playerStoryboardSpecRenderer"`
	} `json:"storyboards"`
	Microformat struct {
		PlayerMicroformatRenderer struct {
			Thumbnail struct {
				Thumbnails []struct {
					URL    string `json:"url"`
					Width  int    `json:"width"`
					Height int    `json:"height"`
				} `json:"thumbnails"`
			} `json:"thumbnail"`
			Embed struct {
				IframeURL      string `json:"iframeUrl"`
				FlashURL       string `json:"flashUrl"`
				Width          int    `json:"width"`
				Height         int    `json:"height"`
				FlashSecureURL string `json:"flashSecureUrl"`
			} `json:"embed"`
			Title struct {
				SimpleText string `json:"simpleText"`
			} `json:"title"`
			Description struct {
				SimpleText string `json:"simpleText"`
			} `json:"description"`
			LengthSeconds      string   `json:"lengthSeconds"`
			OwnerProfileURL    string   `json:"ownerProfileUrl"`
			ExternalChannelID  string   `json:"externalChannelId"`
			AvailableCountries []string `json:"availableCountries"`
			IsUnlisted         bool     `json:"isUnlisted"`
			HasYpcMetadata     bool     `json:"hasYpcMetadata"`
			ViewCount          string   `json:"viewCount"`
			Category           string   `json:"category"`
			PublishDate        string   `json:"publishDate"`
			OwnerChannelName   string   `json:"ownerChannelName"`
			UploadDate         string   `json:"uploadDate"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`
}

type Format struct {
	ItagNo           int    `json:"itag"`
	URL              string `json:"url"`
	MimeType         string `json:"mimeType"`
	Quality          string `json:"quality"`
	Cipher           string `json:"signatureCipher"`
	Bitrate          int    `json:"bitrate"`
	FPS              int    `json:"fps"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	LastModified     string `json:"lastModified"`
	ContentLength    string `json:"contentLength"`
	QualityLabel     string `json:"qualityLabel"`
	ProjectionType   string `json:"projectionType"`
	AverageBitrate   int    `json:"averageBitrate"`
	AudioQuality     string `json:"audioQuality"`
	ApproxDurationMs string `json:"approxDurationMs"`
	AudioSampleRate  string `json:"audioSampleRate"`
	AudioChannels    int    `json:"audioChannels"`

	// InitRange is only available for adaptive formats
	InitRange *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"initRange"`

	// IndexRange is only available for adaptive formats
	IndexRange *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"indexRange"`
}

func (v *Youtube) GetSelf() string {
	return "Youtube"
}

func (v *Youtube) GetCount() int {
	return 1
}

func (v *Youtube) GetInfo(url string) (string, error) {
	id, err := extractVideoID(url)
	if err != nil {
		return "", fmt.Errorf("extractVideoID failed: %w", err)
	}
	ck := baseHeader
	if v.cookie != "" {
		ck["cookie"] = v.cookie
	}

	// Circumvent age restriction to pretend access through googleapis.com
	dest := fmt.Sprintf("https://youtube.com/get_video_info?video_id=%s&eurl=https://youtube.googleapis.com/v/%s", id, id)
	param, err := json.Marshal(map[string]interface{}{
		"url":     dest,
		"method":  "get",
		"headers": ck,
	})
	body, err := query(cfRelay, param)
	if err != nil {
		return "", err
	}
	err = v.parseVideoInfo(body)
	if err != nil {
		return "", err
	}

	//return v,
	return v.GetTitle(0) + v.GetMeta(), nil
}

func (v *Youtube) GetTitle(_ int) string {
	return v.VideoInfo.VideoDetails.Title
}

func (v *Youtube) GetMeta() string {
	details := v.VideoInfo.VideoDetails
	logs := 0
	thumb := details.Thumbnail.Thumbnails[0].URL
	for _, v := range details.Thumbnail.Thumbnails {
		if v.Width > logs {
			logs = v.Width
			thumb = v.URL
		}
	}
	return fmt.Sprintf("\n\nVideoID: %s\nChannelID: %s\n作者: %s\n时长: %s\n观看数: %s\n\nCover: %s",
		details.VideoID, details.ChannelID, details.Author, v.Duration.String(),
		details.ViewCount, thumb)
}

func (v *Youtube) GetSubTitle(_ int) string {
	return ""
}

func (v *Youtube) SetCookie(s string) {
	v.cookie = s
}

func (v *Youtube) GetDownInfo(_ int) ([]string, []int, error) {
	var rates []string
	var p []int
	for _, v := range v.Formats {
		if strings.Contains(v.MimeType, "video") {
			mt := v.MimeType[strings.Index(v.MimeType, "codecs=")+7:]
			mt = mt[1 : len(mt)-1]
			rates = append(rates, fmt.Sprintf("%dx%dp%d - %s - %dk", v.Width, v.Height, v.FPS, mt, v.Bitrate/1000))
		}
		p = append(p, v.ItagNo)
	}
	return rates, p, nil
}

func (v *Youtube) handleURL(s, c string) string {
	if s != "" {
		return s
	} else {
		var err error
		link, err := decipherURL(v.VideoInfo.VideoDetails.VideoID, c)
		if err != nil {
			return ""
		}
		return link
	}
}

func (v *Youtube) GetHeight(band string) int32 {
	for _, s := range v.Formats {
		if strconv.Itoa(s.ItagNo) == band {
			return int32(s.Height)
		}
	}
	return 0
}

func (v *Youtube) GetWidth(band string) int32 {
	for _, s := range v.Formats {
		if strconv.Itoa(s.ItagNo) == band {
			return int32(s.Width)
		}
	}
	return 0
}

func (v *Youtube) GetDuring() int32 {
	sec, err := strconv.Atoi(v.VideoInfo.VideoDetails.LengthSeconds)
	if err != nil {

		return 0
	}
	return int32(sec)
}

func (v *Youtube) Download(ctx context.Context, band string, s chan int) (string, error) {
	var videoLink string
	var tp string
	for _, s := range v.Formats {
		if strconv.Itoa(s.ItagNo) == band {
			tp = s.MimeType
			videoLink = v.handleURL(s.URL, s.Cipher)
		}
	}
	var audioLink string
	if !strings.Contains(tp, ",") {
		logs := 0
		for _, s := range v.Formats {
			if strings.Contains(s.MimeType, "audio") {
				if s.Bitrate > logs {
					logs = s.Bitrate
					if s.URL != "" {
						audioLink = s.URL
					} else {
						audioLink = v.handleURL(s.URL, s.Cipher)
					}
				}
			}
		}
		if audioLink == "" {
			return "", fmt.Errorf("unable to get audio download url")
		}
	}
	if videoLink == "" {
		return "", fmt.Errorf("unable to get steam download url")
	}

	audioFile := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".m4a")
	videoFile := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".m4v")
	video := path.Join(os.TempDir(), strconv.Itoa(rand.Int())+".mp4")
	ck := baseHeader
	if v.cookie != "" {
		ck["cookie"] = v.cookie
	}
	ck["referer"] = "https://www.youtube.com/"

	log.Print(videoLink, audioLink, s, ck)
	err := dlHandler(ctx, videoFile, videoLink, ck, s)
	if err != nil {
		log.Println(err)
		_ = os.Remove(videoFile)
		return "", err
	}
	if audioLink != "" {
		err := dlHandler(ctx, audioFile, audioLink, ck, s)
		if err != nil {
			_ = os.Remove(audioFile)
			return "", err
		}
		cmd := exec.Command("ffmpeg", "-i", videoFile, "-i", audioFile, "-c", "copy", video)
		_ = cmd.Run()
	} else {
		_ = os.Rename(videoFile, video)
	}
	return video, nil
}

func (list ytFormatList) FindByQuality(quality string) *Format {
	for i := range list {
		if list[i].Quality == quality {
			return &list[i]
		}
	}
	return nil
}

func (list ytFormatList) FindByItag(itagNo int) *Format {
	for i := range list {
		if list[i].ItagNo == itagNo {
			return &list[i]
		}
	}
	return nil
}

var (
	//ErrCipherNotFound             = errors.New("cipher not found")
	ErrInvalidCharactersInVideoID = errors.New("invalid characters in video id")
	ErrVideoIDMinLength           = errors.New("the video id must be at least 10 characters long")
	//ErrReadOnClosedResBody        = errors.New("http: read on closed response body")
)

type ErrResponseStatus struct {
	Status string
	Reason string
}

func (err ErrResponseStatus) Error() string {
	if err.Status == "" {
		return "no response status found in the server's answer"
	}

	if err.Reason == "" {
		return fmt.Sprintf("response status: '%s', no reason given", err.Status)
	}

	return fmt.Sprintf("response status: '%s', reason: '%s'", err.Status, err.Reason)
}

type ErrPlayabiltyStatus struct {
	Status string
	Reason string
}

func (err ErrPlayabiltyStatus) Error() string {
	return fmt.Sprintf("cannot playback and download, status: %s, reason: %s", err.Status, err.Reason)
}

// ErrUnexpectedStatusCode is returned on unexpected HTTP status codes
type ErrUnexpectedStatusCode int

func (err ErrUnexpectedStatusCode) Error() string {
	return fmt.Sprintf("unexpected status code: %d", err)
}

func (v *Youtube) parseVideoInfo(info string) error {
	answer, err := url.ParseQuery(info)
	if err != nil {
		return err
	}

	status := answer.Get("status")
	if status != "ok" {
		return &ErrResponseStatus{
			Status: status,
			Reason: answer.Get("reason"),
		}
	}

	// read the streams map
	playerResponse := answer.Get("player_response")
	if playerResponse == "" {
		return errors.New("no player_response found in the server's answer")
	}

	var prData ytPlayerResponseData
	if err := json.Unmarshal([]byte(playerResponse), &prData); err != nil {
		return fmt.Errorf("unable to parse player response JSON: %w", err)
	}

	if seconds, _ := strconv.Atoi(prData.Microformat.PlayerMicroformatRenderer.LengthSeconds); seconds > 0 {
		v.Duration = time.Duration(seconds) * time.Second
	}

	// Check if video is downloadable
	if prData.PlayabilityStatus.Status != "OK" {
		return &ErrPlayabiltyStatus{
			Status: prData.PlayabilityStatus.Status,
			Reason: prData.PlayabilityStatus.Reason,
		}
	}
	v.VideoInfo = prData

	// Assign Streams
	v.Formats = append(prData.StreamingData.Formats, prData.StreamingData.AdaptiveFormats...)

	if len(v.Formats) == 0 {
		return errors.New("no formats found in the server's answer")
	}

	return nil
}

var videoRegexpList = []*regexp.Regexp{
	regexp.MustCompile(`(?:v|embed|watch\?v)[=/]([^"&?/=%]{11})`),
	regexp.MustCompile(`[=/]([^"&?/=%]{11})`),
	regexp.MustCompile(`([^"&?/=%]{11})`),
}

func extractVideoID(videoID string) (string, error) {
	if strings.Contains(videoID, "youtu") || strings.ContainsAny(videoID, "\"?&/<%=") {
		for _, re := range videoRegexpList {
			if isMatch := re.MatchString(videoID); isMatch {
				subs := re.FindStringSubmatch(videoID)
				videoID = subs[1]
			}
		}
	}

	if strings.ContainsAny(videoID, "?&/<%=") {
		return "", ErrInvalidCharactersInVideoID
	}
	if len(videoID) < 10 {
		return "", ErrVideoIDMinLength
	}

	return videoID, nil
}

type operation func([]byte) []byte

func newSpliceFunc(pos int) operation {
	return func(bs []byte) []byte {
		return bs[pos:]
	}
}

func newSwapFunc(arg int) operation {
	return func(bs []byte) []byte {
		pos := arg % len(bs)
		bs[0], bs[pos] = bs[pos], bs[0]
		return bs
	}
}

func reverseFunc(bs []byte) []byte {
	l, r := 0, len(bs)-1
	for l < r {
		bs[l], bs[r] = bs[r], bs[l]
		l++
		r--
	}
	return bs
}

func decipherURL(videoID string, cipher string) (string, error) {
	queryParams, err := url.ParseQuery(cipher)
	if err != nil {
		return "", err
	}

	/* eg:
	    extract decipher from  https://youtube.com/s/player/4fbb4d5b/player_ias.vflset/en_US/base.js
	    var Mt={
		splice:function(a,b){a.splice(0,b)},
		reverse:function(a){a.reverse()},
		EQ:function(a,b){var c=a[0];a[0]=a[b%a.length];a[b%a.length]=c}};
		a=a.split("");
		Mt.splice(a,3);
		Mt.EQ(a,39);
		Mt.splice(a,2);
		Mt.EQ(a,1);
		Mt.splice(a,1);
		Mt.EQ(a,35);
		Mt.EQ(a,51);
		Mt.splice(a,2);
		Mt.reverse(a,52);
		return a.join("")
	*/

	operations, err := parseDecipherOps(videoID)
	if err != nil {
		return "", err
	}

	// apply operations
	bs := []byte(queryParams.Get("s"))
	for _, op := range operations {
		bs = op(bs)
	}

	decipheredURL := fmt.Sprintf("%s&%s=%s", queryParams.Get("url"), queryParams.Get("sp"), string(bs))
	return decipheredURL, nil
}

const (
	jsvarStr   = "[a-zA-Z_\\$][a-zA-Z_0-9]*"
	reverseStr = ":function\\(a\\)\\{" +
		"(?:return )?a\\.reverse\\(\\)" +
		"\\}"
	spliceStr = ":function\\(a,b\\)\\{" +
		"a\\.splice\\(0,b\\)" +
		"\\}"
	swapStr = ":function\\(a,b\\)\\{" +
		"var c=a\\[0\\];a\\[0\\]=a\\[b(?:%a\\.length)?\\];a\\[b(?:%a\\.length)?\\]=c(?:;return a)?" +
		"\\}"
)

var (
	playerConfigPattern = regexp.MustCompile(`yt\.setConfig\({'PLAYER_CONFIG':(.*)}\);`)
	basejsPattern       = regexp.MustCompile(`"js":"\\/s\\/player(.*)base\.js`)

	actionsObjRegexp = regexp.MustCompile(fmt.Sprintf(
		"var (%s)=\\{((?:(?:%s%s|%s%s|%s%s),?\\n?)+)\\};", jsvarStr, jsvarStr, reverseStr, jsvarStr, spliceStr, jsvarStr, swapStr))

	actionsFuncRegexp = regexp.MustCompile(fmt.Sprintf(
		"function(?: %s)?\\(a\\)\\{"+
			"a=a\\.split\\(\"\"\\);\\s*"+
			"((?:(?:a=)?%s\\.%s\\(a,\\d+\\);)+)"+
			"return a\\.join\\(\"\"\\)"+
			"\\}", jsvarStr, jsvarStr, jsvarStr))

	reverseRegexp = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsvarStr, reverseStr))
	spliceRegexp  = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsvarStr, spliceStr))
	swapRegexp    = regexp.MustCompile(fmt.Sprintf("(?m)(?:^|,)(%s)%s", jsvarStr, swapStr))
)

func parseDecipherOps(videoID string) (operations []operation, err error) {
	if videoID == "" {
		return nil, errors.New("video id is empty")
	}

	embedURL := fmt.Sprintf("https://youtube.com/embed/%s?hl=en", videoID)
	embedBody, err := getURL(embedURL)
	if err != nil {
		return nil, err
	}

	playerConfig := playerConfigPattern.FindString(embedBody)

	// eg: "js":\"\/s\/player\/f676c671\/player_ias.vflset\/en_US\/base.js
	escapedBasejsURL := basejsPattern.FindString(playerConfig)
	// eg: ["js", "\/s\/player\/f676c671\/player_ias.vflset\/en_US\/base.js]
	arr := strings.Split(escapedBasejsURL, ":\"")
	basejsURL := "https://youtube.com" + strings.ReplaceAll(arr[len(arr)-1], "\\", "")
	basejsBody, err := getURL(basejsURL)
	if err != nil {
		return nil, err
	}
	objResult := actionsObjRegexp.FindStringSubmatch(basejsBody)
	funcResult := actionsFuncRegexp.FindStringSubmatch(basejsBody)
	if len(objResult) < 3 || len(funcResult) < 2 {
		return nil, errors.New("error parsing signature tokens")
	}

	obj := objResult[1]
	objBody := objResult[2]
	funcBody := funcResult[1]

	var reverseKey, spliceKey, swapKey string

	if result := reverseRegexp.FindStringSubmatch(objBody); len(result) > 1 {
		reverseKey = result[1]
	}
	if result := spliceRegexp.FindStringSubmatch(objBody); len(result) > 1 {
		spliceKey = result[1]
	}
	if result := swapRegexp.FindStringSubmatch(objBody); len(result) > 1 {
		swapKey = result[1]
	}

	regex, err := regexp.Compile(fmt.Sprintf("(?:a=)?%s\\.(%s|%s|%s)\\(a,(\\d+)\\)", obj, reverseKey, spliceKey, swapKey))
	if err != nil {
		return nil, err
	}

	var ops []operation
	for _, s := range regex.FindAllStringSubmatch(funcBody, -1) {
		switch s[1] {
		case reverseKey:
			ops = append(ops, reverseFunc)
		case swapKey:
			arg, _ := strconv.Atoi(s[2])
			ops = append(ops, newSwapFunc(arg))
		case spliceKey:
			arg, _ := strconv.Atoi(s[2])
			ops = append(ops, newSpliceFunc(arg))
		}
	}
	return ops, nil
}
