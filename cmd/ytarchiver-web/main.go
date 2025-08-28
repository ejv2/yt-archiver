package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	ListenAddr = flag.String("listen", ":80", "Address to listen on, in the format [hostname]:port")
	Root       = flag.String("root", ".", "ytarchiver root directory to load files from")
)

type multiError []error

func (m multiError) Error() string {
	sb := &strings.Builder{}
	for i, e := range m {
		if i != 0 {
			sb.WriteRune('\n')
		}

		fmt.Fprint(sb, e.Error())
	}

	return sb.String()
}

type channelData struct {
	ID   string
	Name string
}

type videoData struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ThumbnailURL string `json:"thumbnail"`
	Duration     string `json:"duration_string"`
	ChannelID    string `json:"channel_id"`
	Timestamp    uint   `json:"timestamp"`
	WasLive      bool   `json:"was_live"`
	Extension    string `json:"ext"`
}

type videoArray []videoData

func (v videoArray) Len() int {
	return len(v)
}

func (v videoArray) Less(i, j int) bool {
	// NOTE: Sorting in reverse here so that most recent timestamp comes first.
	return v[i].Timestamp > v[j].Timestamp
}

func (v videoArray) Swap(i, j int) {
	tmp := v[i]
	v[i] = v[j]
	v[j] = tmp
}

type standardData struct {
	Chans  []channelData
	Videos map[string]videoArray
}

func loadStandardData() (standardData, error) {
	dat := standardData{Videos: make(map[string]videoArray)}
	errs := make(multiError, 0, 4)

	chandirs, err := os.ReadDir(*Root)
	if err != nil {
		return dat, fmt.Errorf("standard data: reading channels: %w", err)
	}

	for _, c := range chandirs {
		if !c.IsDir() {
			continue
		}

		path := filepath.Join(*Root, c.Name(), "channel.json")
		fdat, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("standard data: reading channel data: %w", err))
			continue
		}

		var chanobj channelData
		err = json.Unmarshal(fdat, &chanobj)
		if err != nil {
			errs = append(errs, fmt.Errorf("standard data: parsing channel data: %w", err))
			continue
		}

		dat.Chans = append(dat.Chans, chanobj)

		chanpath := filepath.Join(*Root, c.Name())
		vidfiles, err := os.ReadDir(chanpath)
		if err != nil {
			errs = append(errs, fmt.Errorf("standard data: reading channel videos: %w", err))
			continue
		}

		for _, v := range vidfiles {
			if strings.HasSuffix(v.Name(), ".info.json") {
				path := filepath.Join(*Root, c.Name(), v.Name())
				fdat, err := os.ReadFile(path)
				if err != nil {
					errs = append(errs, fmt.Errorf("standard data: reading video data: %w", err))
					continue
				}

				var video videoData
				err = json.Unmarshal(fdat, &video)
				if err != nil {
					errs = append(errs, fmt.Errorf("standard data: parsing video data: %w", err))
					continue
				}

				dat.Videos[chanobj.ID] = append(dat.Videos[chanobj.ID], video)
			}
		}

		// Sort in descending order of unix timestamp (i.e most recent first)
		sort.Sort(dat.Videos[chanobj.ID])
	}

	if len(errs) != 0 {
		return dat, errs
	}

	return dat, nil
}

// loadStandardDataChannel is kind of lazy and inefficient, but what the hell
func loadStandardDataChannel(cid string) (standardData, int, error) {
	dat, err := loadStandardData()
	if err != nil {
		return dat, -1, err
	}

	chanind := 0
	for i, c := range dat.Chans {
		if c.ID == cid {
			chanind = i
			break
		}
	}

	return dat, chanind, nil
}

// loadStandardDataVideo has the same problems (if not worse) as loadStandardDataChannel,
// but I am tired and can't be bothered.
func loadStandardDataVideo(cid, vid string) (standardData, int, int, error) {
	dat, chanind, err := loadStandardDataChannel(cid)
	if err != nil {
		return dat, -1, -1, err
	}

	vind := -1
	for i, v := range dat.Videos[cid] {
		if v.ID == vid {
			vind = i
			break
		}
	}

	return dat, chanind, vind, nil
}

func limitString(arg string, lim int) string {
	if len(arg) < lim {
		return arg
	}

	return arg[:lim] + "..."
}

func handleRoot(c *gin.Context) {
	dat, err := loadStandardData()
	if err != nil {
		c.AbortWithError(500, err)
	}

	c.HTML(200, "index.gohtml", dat)
}

func handleChannel(c *gin.Context) {
	cid := c.Param("id")
	if cid == "" {
		log.Panicln("got empty ID parameter in required route")
	}

	dat, cind, err := loadStandardDataChannel(cid)
	if err != nil {
		c.AbortWithError(500, err)
	}

	c.HTML(200, "channel.gohtml", struct {
		standardData
		Cid  string
		Cind int
	}{dat, cid, cind})
}

func handleVideo(c *gin.Context) {
	cid := c.Param("cid")
	vid := c.Param("id")
	if cid == "" || vid == "" {
		log.Panicln("got empty ID/VID parameter in required route")
	}

	dat, cind, vind, err := loadStandardDataVideo(cid, vid)
	if err != nil {
		c.AbortWithError(500, err)
	}

	c.HTML(200, "video.gohtml", struct {
		standardData
		Cid  string
		Vid  string
		Cind int
		Vind int
	}{dat, cid, vid, cind, vind})
}

func handleHelp(c *gin.Context) {
	dat, err := loadStandardData()
	if err != nil {
		c.AbortWithError(500, err)
	}

	c.HTML(200, "help.gohtml", dat)
}

func main() {
	log.Println("Starting ytarchiver web interface...")
	flag.Parse()

	// Startup and listen
	router := gin.New()
	srv := http.Server{
		Addr:              *ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	router.Use(gin.Logger(), gin.Recovery())
	router.FuncMap["limit"] = limitString
	router.LoadHTMLGlob("*.gohtml")

	router.GET("/", handleRoot)
	router.GET("/chan/:id", handleChannel)
	router.GET("/vid/:cid/:id", handleVideo)
	router.GET("/help", handleHelp)
	router.Static("/videos/", *Root)

	errchan := make(chan error, 1)
	sigchan := make(chan os.Signal, 1)

	signal.Notify(sigchan, os.Interrupt)
	go func() {
		err := srv.ListenAndServe()
		errchan <- err
	}()

	select {
	case <-sigchan:
		log.Println("Caught interrupt signal. Terminating gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(ctx)
		if err != nil {
			if err == ctx.Err() {
				log.Println("Shutdown timeout reached. Terminating forcefully...")
				return
			}
			log.Fatal(err)
		}
	case err := <-errchan:
		if err != http.ErrServerClosed {
			log.Panic(err) // NOTREACHED: unless fatal error
		}
	}
}
