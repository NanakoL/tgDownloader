package main

import (
	"bytes"
	"fmt"
	"github.com/NanakoL/mtproto"
	"github.com/ansel1/merry"
	log "github.com/sirupsen/logrus"
	"io"
	"main/extractors"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	m          *mtproto.MTProto
	activeTask sync.Map
	userCookie sync.Map
)

const mutexLocked = 1 << iota

type Mutex struct {
	sync.Mutex
}

func (m *Mutex) TryLock() bool {
	return atomic.CompareAndSwapInt32((*int32)(unsafe.Pointer(&m.Mutex)), 0, mutexLocked)
}

type wrapper struct {
	extractors.API
	downLock  Mutex
	userID    int32
	downMsgID int32
	initTime  time.Time
	msgID     int32
	currentP  int
}

func main() {
	//go pendingListManager()
	go cleaner()
	log.SetLevel(log.InfoLevel)
	if err := start(appID, appHash, token); err != nil {
		log.Fatal(merry.Details(err))
	}
}

func cleaner() {
	for {
		var delList []interface{}
		activeTask.Range(func(key, value interface{}) bool {
			val := value.(*wrapper)
			if ok := val.downLock.TryLock(); ok {
				if time.Now().Unix() - val.initTime.Unix() > 12800 {
					// cleanup
					delList = append(delList, key)
				}
			}
			val.downLock.Unlock()
			return true
		})
		for _, v := range delList {
			if s, ok := activeTask.Load(v); ok {
				val := s.(*wrapper)
				if ok := val.downLock.TryLock(); ok {
					activeTask.Delete(v)
				}
				val.downLock.Unlock()
			}
		}
		time.Sleep(time.Hour)
	}
}

func start(appID int32, appHash, token string) error {

	config := mtproto.NewAppConfig(appID, appHash)

	session, err := mtproto.LoadSession("cred.json")
	if err != nil {
		panic(err)
	}
	//dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:7890", nil, proxy.Direct)
	//if err != nil {
	//	log.Println("can't connect to the proxy:", err)
	//	os.Exit(1)
	//}
	m = mtproto.NewMTProto(config)
	//m.SetDialer(dialer)
	m.SetSession(session)
	m.UseIPv6(true)

	if err := m.InitSessAndConnect(); err != nil {
		return merry.Wrap(err)
	}

	for {
		res := m.SendSync(mtproto.UpdatesGetState{})
		if mtproto.IsErrorType(res, mtproto.ErrUnauthorized) {
			if err := m.AuthBot(token); err != nil {
				log.Error(merry.Wrap(err))
			}
			continue
		}
		_, ok := res.(mtproto.UpdatesState)
		if !ok {
			log.Error(mtproto.WrongRespError(res))
			continue
		}
		break
	}

	log.Println("Seems authed.")
	_ = m.CopySession().Save("cred.json")
	m.SetEventsHandler(updateHandler)

	<-chan bool(nil) //pausing forever
	return nil
}

func updateHandler(updateTL mtproto.TL) {
	switch update := updateTL.(type) {
	case mtproto.UpdateBotCallbackQuery:
		log.Printf("Callback %+v\n", string(update.Data))
		go newCallbackHandler(update)
	case mtproto.UpdateNewMessage:
		log.Printf("Message %+v\n", update.Message)
		go newMessageHandler(update.Message)
	case mtproto.Updates:
		for _, item := range update.Updates {
			updateHandler(item)
		}

	default:
		log.Printf("Unknown Updates: %T", update)
	}
}

func progUpdater(c chan int, msgID int32, userID int32, cont string, tcl int64) {
	var total int
	for w := range c {
		total += w
		var msg string
		if tcl != 0 {
			p := extractors.ByteCountIEC(tcl)
			msg = fmt.Sprintf("%s...(%s/%s)", cont, extractors.ByteCountIEC(int64(total)), p)
		} else {
			msg = fmt.Sprintf("%s...(%s)", cont, extractors.ByteCountIEC(int64(total)))
		}
		m.Send(mtproto.MessagesEditMessage{
			Flags:   1 << 11,
			Peer:    mtproto.InputPeerUser{UserID: userID},
			ID:      msgID,
			Message: msg,
		})
	}
}

func newCallbackHandler(update mtproto.UpdateBotCallbackQuery) {
	if string(update.Data) == "null" {
		m.Send(mtproto.MessagesSetBotCallbackAnswer{
			Flags:   1 << 0,
			QueryID: update.QueryID,
			Message: "再按也不会有效果的哟",
		})
		return
	}
	if string(update.Data) == "removeCookie" {
		userCookie.Delete(update.UserID)
		m.Send(mtproto.MessagesSetBotCallbackAnswer{
			Flags:   1 << 0,
			QueryID: update.QueryID,
			Message: "cookie已清理.",
		})
		return
	}
	parse := strings.Split(string(update.Data), ",")
	if len(parse) < 2 {
		m.Send(mtproto.MessagesSetBotCallbackAnswer{
			Flags:   1 << 0,
			QueryID: update.QueryID,
			Message: "Callback请求无效",
		})
		return
	}
	reqID := parse[0]
	action := parse[1]
	if val, ok := activeTask.Load(reqID); ok {
		res := val.(*wrapper)
		if ok := res.downLock.TryLock(); !ok {
			m.Send(mtproto.MessagesSetBotCallbackAnswer{
				Flags:   1 << 0,
				QueryID: update.QueryID,
				Message: "Err/当前已有正在运行的请求线程",
			})
			return
		}
		defer res.downLock.Unlock()
		if action == "download" {
			queryHandler(res, update.UserID, reqID)
			return
		}
		if action == "d2d3" {
			band := parse[2]
			downloadHandler(res, update.UserID, band)
			return
		}
		if action == "select" {
			selectHandler(res, update.UserID, update.QueryID, reqID)
			return
		}
		if action == "page" {
			page, err := strconv.Atoi(parse[2])
			if err != nil {
				return
			}
			pageHandler(res, update.UserID, page, reqID)
			return
		}
		if action == "s4s5" {
			sel, err := strconv.Atoi(parse[2])
			if err != nil {
				return
			}
			pHandler(res, update.UserID, sel, reqID)
			return
		}
		if action == "deletedown" {
			m.Send(mtproto.MessagesDeleteMessages{
				Flags: 1 << 0,
				ID:    []int32{res.downMsgID},
			})
			return
		}
	} else {
		m.Send(mtproto.MessagesSetBotCallbackAnswer{
			Flags:   1 << 0,
			QueryID: update.QueryID,
			Message: "Task已过期",
		})
		return
	}
}

func pHandler(res *wrapper, userID int32, sel int, reqID string) {
	res.currentP = sel
	rows := []mtproto.KeyboardButtonRow{
		{Buttons: []mtproto.TL{
			mtproto.KeyboardButtonCallback{
				Text: "下载",
				Data: []byte(fmt.Sprintf("%s,download", reqID)),
			},
			mtproto.KeyboardButtonCallback{
				Text: "分P选择",
				Data: []byte(fmt.Sprintf("%s,select", reqID)),
			},
		}},
	}

	m.Send(mtproto.MessagesEditMessage{
		Flags:   1<<11 | 1<<2,
		Peer:    mtproto.InputPeerUser{UserID: userID},
		ID:      res.msgID,
		Message: res.GetTitle(sel) + res.GetMeta(),
		ReplyMarkup: mtproto.ReplyInlineMarkup{
			Rows: mtproto.SliceToTLStable(rows),
		},
	})
}

func pageHandler(res *wrapper, userID int32, page int, reqID string) {
	offset := (page - 1) * 5
	max := offset + 5
	if res.GetCount() <= max {
		max = res.GetCount()
	}
	var rows []mtproto.KeyboardButtonRow
	for i := offset; i < max; i++ {
		title := res.GetTitle(i)
		rows = append(rows, mtproto.KeyboardButtonRow{
			Buttons: []mtproto.TL{
				mtproto.KeyboardButtonCallback{
					Text: fmt.Sprintf("P%d - %s", i+1, title),
					Data: []byte(fmt.Sprintf("%s,s4s5,%d", reqID, i)),
				},
			},
		})
	}
	bts := []mtproto.TL{
		mtproto.KeyboardButtonCallback{
			Text: fmt.Sprintf("第%d页", page),
			Data: []byte("null"),
		},
	}
	if page > 1 {
		bts = append(bts, mtproto.KeyboardButtonCallback{
			Text: "上一页",
			Data: []byte(fmt.Sprintf("%s,page,%d", reqID, page-1)),
		})
	}
	if res.GetCount() > max {
		bts = append(bts, mtproto.KeyboardButtonCallback{
			Text: "下一页",
			Data: []byte(fmt.Sprintf("%s,page,%d", reqID, page+1)),
		})
	}

	rows = append(rows, mtproto.KeyboardButtonRow{
		Buttons: bts,
	})
	m.Send(mtproto.MessagesEditMessage{
		Flags:   1<<11 | 1<<2,
		Peer:    mtproto.InputPeerUser{UserID: userID},
		ID:      res.msgID,
		Message: "从下列选项中选择一个分P",
		ReplyMarkup: mtproto.ReplyInlineMarkup{
			Rows: mtproto.SliceToTLStable(rows),
		},
	})
}

func selectHandler(res *wrapper, userID int32, queryID int64, reqID string) {
	if res.GetCount() == 1 {
		m.Send(mtproto.MessagesSetBotCallbackAnswer{
			Flags:   1 << 0,
			QueryID: queryID,
			Message: "这个视频只有1P啦",
		})
		return
	}
	max := 5
	if res.GetCount() <= 5 {
		max = res.GetCount()
	}
	var rows []mtproto.KeyboardButtonRow
	for i := 0; i < max; i++ {
		title := res.GetSubTitle(i)
		rows = append(rows, mtproto.KeyboardButtonRow{
			Buttons: []mtproto.TL{
				mtproto.KeyboardButtonCallback{
					Text: fmt.Sprintf("P%d - %s", i+1, title),
					Data: []byte(fmt.Sprintf("%s,s4s5,%d", reqID, i)),
				},
			},
		})
	}
	if res.GetCount() > 5 {
		rows = append(rows, mtproto.KeyboardButtonRow{
			Buttons: []mtproto.TL{
				mtproto.KeyboardButtonCallback{
					Text: "第1页",
					Data: []byte("null"),
				},
				mtproto.KeyboardButtonCallback{
					Text: "下一页",
					Data: []byte(fmt.Sprintf("%s,page,2", reqID)),
				},
			},
		})
	}
	m.SendSync(mtproto.MessagesEditMessage{
		Flags:   1<<11 | 1<<2,
		Peer:    mtproto.InputPeerUser{UserID: userID},
		ID:      res.msgID,
		Message: fmt.Sprintf("%s\n\n从下列选项中选择一个分P", res.GetTitle(res.currentP)),
		ReplyMarkup: mtproto.ReplyInlineMarkup{
			Rows: mtproto.SliceToTLStable(rows),
		},
	})
}

func queryHandler(res *wrapper, userID int32, reqID string) {
	l := m.SendSync(mtproto.MessagesSendMessage{
		Peer:     mtproto.InputPeerUser{UserID: userID},
		RandomID: rand.Int63(),
		Message:  "Preparing...",
	})
	res.downMsgID = getMsgID(l)
	res.userID = userID
	msg, cho, err := res.GetDownInfo(res.currentP)
	if err != nil {
		m.Send(mtproto.MessagesEditMessage{
			Flags:   1 << 11,
			Peer:    mtproto.InputPeerUser{UserID: userID},
			ID:      res.downMsgID,
			Message: fmt.Sprintf("Download Failed: %v", err),
		})
		return
	}
	var rows []mtproto.KeyboardButtonRow
	for c, item := range msg {
		rows = append(rows, mtproto.KeyboardButtonRow{
			Buttons: []mtproto.TL{
				mtproto.KeyboardButtonCallback{
					Text: item,
					Data: []byte(fmt.Sprintf("%s,d2d3,%s", reqID, strconv.Itoa(cho[c]))),
				},
			},
		})
	}
	rows = append(rows, mtproto.KeyboardButtonRow{
		Buttons: []mtproto.TL{
			mtproto.KeyboardButtonCallback{
				Text: "取消操作",
				Data: []byte(fmt.Sprintf("%s,deletedown", reqID)),
			},
		},
	})
	m.Send(mtproto.MessagesEditMessage{
		Flags:   1<<11 | 1<<2,
		Peer:    mtproto.InputPeerUser{UserID: userID},
		ID:      res.downMsgID,
		Message: fmt.Sprintf("%s\n\n从下列选项中选择一个进行下载:", res.GetTitle(res.currentP)),
		ReplyMarkup: mtproto.ReplyInlineMarkup{
			Rows: mtproto.SliceToTLStable(rows),
		},
	})
}

func downloadHandler(res *wrapper, userID int32, band string) {
	m.SendSync(mtproto.MessagesEditMessage{
		Flags:   1 << 11,
		Peer:    mtproto.InputPeerUser{UserID: userID},
		ID:      res.downMsgID,
		Message: fmt.Sprintf("Preparing..."),
	})
	tuns := make(chan int)
	go progUpdater(tuns, res.downMsgID, userID, "Downloading", 0)
	f, err := res.Download(band, tuns)
	if err != nil {
		m.SendSync(mtproto.MessagesEditMessage{
			Flags:   1 << 11,
			Peer:    mtproto.InputPeerUser{UserID: userID},
			ID:      res.downMsgID,
			Message: fmt.Sprintf("Download Failed"),
		})
		_ = os.Remove(f)
		return
	}
	info, err := os.Stat(f)
	if err != nil {
		_ = os.Remove(f)
		return
	}
	m.Send(mtproto.MessagesEditMessage{
		Flags:   1 << 11,
		Peer:    mtproto.InputPeerUser{UserID: userID},
		ID:      res.downMsgID,
		Message: fmt.Sprintf("Uploading...(%s/%s)", extractors.ByteCountIEC(0), extractors.ByteCountIEC(info.Size())),
	})
	wg := new(sync.WaitGroup)
	if info.Size() > 1.99*1000*1000*1000 {
		m.SendSync(mtproto.MessagesEditMessage{
			Flags:   1 << 11,
			Peer:    mtproto.InputPeerUser{UserID: userID},
			ID:      res.downMsgID,
			Message: fmt.Sprintf("文件大小过大：无法上传至Telegram，已保存在本地。"),
		})
	} else {
		wg.Add(1)
		go uploadHandler(res, userID, info, band, f, wg)
	}
	go wgHandler(wg, f)
}

func wgHandler(wg *sync.WaitGroup, f string) {
	wg.Wait()
	_ = os.Remove(f)
}

func uploadHandler(res *wrapper, userID int32, info os.FileInfo, band, f string, wg *sync.WaitGroup) {
	defer wg.Done()
	file, err := os.Open(f)
	if err != nil {
		return
	}
	tuns := make(chan int)
	go progUpdater(tuns, res.downMsgID, userID, "Uploading", info.Size())
	fl := uploadBigFile(file, info.Size(), tuns)
	fl.Name = info.Name()
	_ = file.Close()

	var image *mtproto.InputFile
	frame, err := getFrame(f)
	if err == nil {
		// parse
		src := uploadSmallFile(frame, int64(frame.Len()))
		src.Name = strconv.Itoa(rand.Int())
		image = &src
	} else {
		log.Println(err)
	}

	attr := mtproto.DocumentAttributeVideo{
		Flags:             1 << 1,
		SupportsStreaming: true,
		Duration:          res.GetDuring(),
		H:                 res.GetHeight(band),
		W:                 res.GetWidth(band),
	}

	m.Send(mtproto.MessagesDeleteMessages{
		Flags: 1 << 0,
		ID:    []int32{res.downMsgID},
	})
	fn := fmt.Sprintf("%s - %dx%d - %s", res.GetTitle(res.currentP), res.GetWidth(band), res.GetHeight(band), band)

	media := mtproto.InputMediaUploadedDocument{
		File:       fl,
		MimeType:   "video/mp4",
		Attributes: []mtproto.TL{attr},
	}

	if image != nil {
		media.Flags = 1 << 2
		media.Thumb = *image
	}

	resp := m.Send(mtproto.MessagesSendMedia{
		Peer:     mtproto.InputPeerUser{UserID: userID},
		RandomID: rand.Int63(),
		Message:  fn,
		Media:    media,
	})

	log.Println(resp)
}

func newMessageHandler(message mtproto.TL) {
	var msg mtproto.Message
	if _, ok := message.(mtproto.Message); ok {
		msg = message.(mtproto.Message)
	}
	if _, ok := msg.ToID.(mtproto.PeerUser); ok {

		if msg.Message == "/start" {
			rand.Seed(time.Now().UnixNano())
			m.Send(mtproto.MessagesSendMessage{
				Peer:     mtproto.InputPeerUser{UserID: msg.FromID},
				RandomID: rand.Int63(),
				Message:  "本bot什么都不会，Sako也是。\n\nGithub: https://github.com/NanakoL/tgDownloader",
			})
			return
		}

		if msg.Message == "/cookie" {
			be := fmt.Sprintf("通过此选项可以设置一个临时Cookie（用于获取特殊视频等）" +
				"，由于Cookie不会在磁盘存储，定期重启/维护等操作后数据会丢失（需要重新设置）。")
			if v, ok := userCookie.Load(msg.FromID); ok {
				be += "\n\n当前设置的Cookie:\n\n" + v.(string)
				rows := []mtproto.KeyboardButtonRow{
					{Buttons: []mtproto.TL{
						mtproto.KeyboardButtonCallback{
							Text: "删除Cookie",
							Data: []byte("removeCookie"),
						},
					}},
				}
				m.Send(mtproto.MessagesSendMessage{
					Flags:    1 << 2,
					Peer:     mtproto.InputPeerUser{UserID: msg.FromID},
					RandomID: rand.Int63(),
					Message:  be,
					ReplyMarkup: mtproto.ReplyInlineMarkup{
						Rows: mtproto.SliceToTLStable(rows),
					},
				})
			} else {
				m.Send(mtproto.MessagesSendMessage{
					Peer:     mtproto.InputPeerUser{UserID: msg.FromID},
					RandomID: rand.Int63(),
					Message:  be,
				})
			}
		}

		if strings.HasPrefix(msg.Message, "/cookie ") {
			ck := strings.Replace(msg.Message, "/cookie ", "", -1)
			if ck == "" {
				m.Send(mtproto.MessagesSendMessage{
					Peer:     mtproto.InputPeerUser{UserID: msg.FromID},
					RandomID: rand.Int63(),
					Message:  fmt.Sprintf("Cookie输入为空！请检查后重新输入。"),
				})
				return
			}
			userCookie.Store(msg.FromID, ck)
			m.Send(mtproto.MessagesSendMessage{
				Peer:     mtproto.InputPeerUser{UserID: msg.FromID},
				RandomID: rand.Int63(),
				Message: fmt.Sprintf("Cookie已设置为:\n\n%s\n\n"+
					"注意由于cookie仅保存在内存中，定期重启或维护后会丢失，需要重新设置", ck),
			})
			return
		}

		if api := extractors.Resolver(msg.Message); api != nil {
			baseMessageTL := m.SendSync(mtproto.MessagesSendMessage{
				Peer:     mtproto.InputPeerUser{UserID: msg.FromID},
				RandomID: rand.Int63(),
				Message:  "数据获取中...",
			})
			msgID := getMsgID(baseMessageTL)
			if msgID == 0 {
				return
			}
			if ck, ok := userCookie.Load(msg.FromID); ok {
				api.SetCookie(ck.(string))
			}
			sem, err := api.GetInfo(msg.Message)

			if err != nil {
				m.Send(mtproto.MessagesEditMessage{
					Flags:   1 << 11,
					Peer:    mtproto.InputPeerUser{UserID: msg.FromID},
					ID:      msgID,
					Message: fmt.Sprintf("出现错误： %s", err.Error()),
				})
			} else {
				reqID := strconv.Itoa(rand.Int())
				activeTask.Store(reqID, &wrapper{
					API:      api,
					initTime: time.Now(),
					msgID:    msgID,
					currentP: 0,
				})

				rows := []mtproto.KeyboardButtonRow{
					{Buttons: []mtproto.TL{
						mtproto.KeyboardButtonCallback{
							Text: "下载",
							Data: []byte(fmt.Sprintf("%s,download", reqID)),
						},
						mtproto.KeyboardButtonCallback{
							Text: "分P选择",
							Data: []byte(fmt.Sprintf("%s,select", reqID)),
						},
					}},
				}

				m.Send(mtproto.MessagesEditMessage{
					Flags:   1<<11 | 1<<2,
					Peer:    mtproto.InputPeerUser{UserID: msg.FromID},
					ID:      msgID,
					Message: sem,
					ReplyMarkup: mtproto.ReplyInlineMarkup{
						Rows: mtproto.SliceToTLStable(rows),
					},
				})
			}
		}

		return
	}
}

func getMsgID(m mtproto.TL) int32 {
	switch w := m.(type) {
	case mtproto.Message:
		return w.ID
	case mtproto.UpdateShortSentMessage:
		return w.ID
	case mtproto.UpdateShortMessage:
		return w.ID
	}
	return 0
}

//func uploadBigFile(w chan int, file *os.File, info os.FileInfo) mtproto.InputFileBig {
func uploadBigFile(file io.Reader, size int64, p ...chan int) mtproto.InputFileBig {
	pipe := false
	if len(p) > 0 {
		pipe = true
	}
	//size := info.Size()
	fileID := rand.Int63()
	defer func() {
		if pipe {
			close(p[0])
		}
	}()

	ps := int32(0)
	bs := []int64{524288, 262144, 131072, 65536, 32768, 16384, 8192, 4096, 1024, 512, 64, 2}
	var bsf int32
	for _, p := range bs {
		if size/p < 3000 && size/p > 2 {
			bsf = int32(p)
			break
		}
	}
	total := int32(math.Ceil(float64(size) / float64(bsf)))
	buf := make([]byte, bsf)
	for {
		n, err := file.Read(buf)
		if err != nil {
			if err != io.EOF {
				panic(err)
			}
			break
		}
		log.Debug("Send FilePart: ", fileID, ps, total, n)
		res := m.SendSyncRetry(mtproto.UploadSaveBigFilePart{
			FileID:         fileID,
			FilePart:       ps,
			FileTotalParts: total,
			Bytes:          buf[:n],
		}, time.Second, 5, time.Minute)
		log.Debug(res)
		ps++
		if pipe {
			p[0] <- n
		}
	}

	res := mtproto.InputFileBig{
		ID:    fileID,
		Parts: total,
	}
	return res
}

func uploadSmallFile(file io.Reader, size int64, p ...chan int) mtproto.InputFile {
	pipe := false
	if len(p) > 0 {
		pipe = true
	}
	//size := info.Size()
	fileID := rand.Int63()
	defer func() {
		if pipe {
			close(p[0])
		}
	}()

	ps := int32(0)
	bs := []int64{524288, 262144, 131072, 65536, 32768, 16384, 8192, 4096, 1024, 512, 64, 2}
	var bsf int32
	for _, p := range bs {
		if size/p < 3000 && size/p > 2 {
			bsf = int32(p)
			break
		}
	}
	total := int32(math.Ceil(float64(size) / float64(bsf)))
	buf := make([]byte, bsf)
	for {
		n, err := file.Read(buf)
		if err != nil {
			if err != io.EOF {
				panic(err)
			}
			break
		}
		log.Debug("Send FilePart: ", fileID, ps, total, n)
		res := m.SendSyncRetry(mtproto.UploadSaveFilePart{
			FileID:   fileID,
			FilePart: ps,
			Bytes:    buf[:n],
		}, time.Second, 5, time.Minute)
		log.Debug(res)
		ps++
		if pipe {
			p[0] <- n
		}
	}

	res := mtproto.InputFile{
		ID:    fileID,
		Parts: total,
	}
	return res
}

func getFrame(filename string) (*bytes.Buffer, error) {
	cmd := exec.Command("ffmpeg", "-i", filename, "-vframes", "1", "-f", "singlejpeg", "-")
	buf := new(bytes.Buffer)
	cmd.Stdout = buf

	if cmd.Run() != nil {
		return nil, fmt.Errorf("could not generate frame")
	}

	return buf, nil
}
