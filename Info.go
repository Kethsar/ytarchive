package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dannav/hhmmss"
	"github.com/xhit/go-str2duration/v2"
)

const (
	DtypeAudio            = "audio"
	DtypeVideo            = "video"
	AudioItag             = 140
	AudioOnlyQuality      = 0
	BufferSize            = 8192
	DefaultFilenameFormat = "%(title)s-%(id)s"
	// 5 days in seconds
	LiveMaximumSeekable = 432000
)

type VideoItag struct {
	H264 int
	VP9  int
}

// https://gist.github.com/AgentOak/34d47c65b1d28829bb17c24c04a0096f
var (
	FilenameFormatBlacklist = []string{
		"description",
	}

	VideoLabelItags = map[string]VideoItag{
		"audio_only": {H264: 0, VP9: 0},
		"144p":       {H264: 160, VP9: 278},
		"240p":       {H264: 133, VP9: 242},
		"360p":       {H264: 134, VP9: 243},
		"480p":       {H264: 135, VP9: 244},
		"720p":       {H264: 136, VP9: 247},
		"720p60":     {H264: 298, VP9: 302},
		"1080p":      {H264: 137, VP9: 248},
		"1080p60":    {H264: 299, VP9: 303},
		"1440p":      {H264: 264, VP9: 271},
		"1440p60":    {H264: 304, VP9: 308},
		"2160p":      {H264: 266, VP9: 313},
		"2160p60":    {H264: 305, VP9: 315},
	}

	VideoQualities = []string{
		"audio_only",
		"144p",
		"240p",
		"360p",
		"480p",
		"720p",
		"720p60",
		"1080p",
		"1080p60",
		"1440p",
		"1440p60",
		"2160p",
		"2160p60",
	}
)

/*
Simple class to more easily keep track of what fields are available for
file name formatting
*/
type FormatInfo map[string]string

/*
Metadata for the final file
*/
type MetaInfo map[string]string

/*
Info to be sent through the progress queue
*/
type ProgressInfo struct {
	Itag      int
	ByteCount int
	MaxSeq    int
	StartFrag int
}

/*
Fragment information/data
*/
type Fragment struct {
	Seq         int
	FileName    string
	XHeadSeqNum int
	Data        *bytes.Buffer
	Slow        bool
	MimeType    string
}

type seqChanInfo struct {
	CurSequence int
	MaxSequence int
}

/*
For sharing state between some functions used for downloading threads
*/
type fragThreadState struct {
	Name         string
	BaseFilePath string
	DataType     string
	SeqNum       int
	MaxSeq       int
	Tries        int
	FullRetries  int
	Is403        bool
	ToFile       bool
	SleepTime    time.Duration
}

type MediaDLInfo struct {
	sync.RWMutex
	ActiveJobs  int
	DownloadURL string
	BasePath    string
	DataType    string
	Finished    bool
	URLHost     string
}

/*
State for resumable downloading
*/
type DownloadState struct {
	StartFrag int
	Fragments int
	Size      int64
	TempDir   string
	File      string `json:"-"`
}

/*
Miscellaneous information
*/
type DownloadInfo struct {
	sync.RWMutex
	FormatInfo FormatInfo
	Metadata   MetaInfo
	CookiesURL *url.URL
	Ytcfg      *YTCFG
	PoToken    string

	Stopping         bool
	InProgress       bool
	Live             bool
	VP9              bool
	H264             bool
	Unavailable      bool
	GVideoDDL        bool
	FragFiles        bool
	LiveURL          bool
	AudioOnly        bool
	VideoOnly        bool
	MembersOnly      bool
	InfoPrinted      bool
	DisableSaveState bool
	LiveFromVal      string
	LiveFromSq       int

	Thumbnail           string
	VideoID             string
	URL                 string
	SelectedQuality     string
	Status              string
	CaptureDurationSecs int
	StartDelaySecs      int

	FragMaxTries   uint
	Wait           int
	Quality        int
	RetrySecs      int
	Jobs           int
	TargetDuration int
	LastSq         int
	LastUpdated    time.Time

	MDLInfo map[string]*MediaDLInfo
	DLState map[int]*DownloadState

	FileMode os.FileMode
	DirMode  os.FileMode
}

func NewDownloadInfo() *DownloadInfo {
	return &DownloadInfo{
		FragFiles:      true,
		Wait:           ActionAsk,
		Quality:        -1,
		Jobs:           1,
		TargetDuration: 5,
		FormatInfo:     NewFormatInfo(),
		Metadata:       NewMetaInfo(),
		MDLInfo: map[string]*MediaDLInfo{
			DtypeVideo: {},
			DtypeAudio: {},
		},
		DLState: make(map[int]*DownloadState),
	}
}

func NewFragThreadState(name, baseFPath, dataType string, toFile bool, sleepTime time.Duration) *fragThreadState {
	return &fragThreadState{
		Name:         name,
		BaseFilePath: baseFPath,
		DataType:     dataType,
		ToFile:       toFile,
		SleepTime:    sleepTime,
	}
}

func NewFormatInfo() FormatInfo {
	return FormatInfo{
		"id":           "",
		"title":        "",
		"channel_id":   "",
		"channel":      "",
		"upload_date":  "",
		"start_date":   "",
		"publish_date": "",
		"description":  "",
		"url":          "",
	}
}

func NewMetaInfo() MetaInfo {
	return MetaInfo{
		"title":   "%(title)s",
		"artist":  "%(channel)s",
		"date":    "%(upload_date)s",
		"comment": "%(url)s\n\n%(description)s",
	}
}

func (di *DownloadInfo) IsStopping() bool {
	di.RLock()
	defer di.RUnlock()
	return di.Stopping
}

func (di *DownloadInfo) Stop() {
	di.Lock()
	defer di.Unlock()
	di.Stopping = true
	di.SetFinished(DtypeAudio)
	di.SetFinished(DtypeVideo)
}

func (di *DownloadInfo) IsLive() bool {
	di.RLock()
	defer di.RUnlock()
	return di.Live
}

func (di *DownloadInfo) IsUnavailable() bool {
	di.RLock()
	defer di.RUnlock()
	return di.Unavailable
}

func (di *DownloadInfo) IsGVideoDDL() bool {
	di.RLock()
	defer di.RUnlock()
	return di.GVideoDDL
}

func (di *DownloadInfo) GetActiveJobCount(dataType string) int {
	di.MDLInfo[dataType].RLock()
	defer di.MDLInfo[dataType].RUnlock()
	return di.MDLInfo[dataType].ActiveJobs
}

func (di *DownloadInfo) IncrementJobs(dataType string) {
	di.MDLInfo[dataType].Lock()
	defer di.MDLInfo[dataType].Unlock()
	di.MDLInfo[dataType].ActiveJobs += 1
}

func (di *DownloadInfo) DecrementJobs(dataType string) {
	di.MDLInfo[dataType].Lock()
	defer di.MDLInfo[dataType].Unlock()
	di.MDLInfo[dataType].ActiveJobs -= 1
}

func (di *DownloadInfo) GetDownloadUrl(dataType string) string {
	di.MDLInfo[dataType].RLock()
	defer di.MDLInfo[dataType].RUnlock()
	return di.MDLInfo[dataType].DownloadURL
}

func (di *DownloadInfo) SetDownloadUrl(dataType, dlURL string) {
	di.MDLInfo[dataType].Lock()
	defer di.MDLInfo[dataType].Unlock()

	purl, err := url.Parse(dlURL)
	if err == nil {
		di.MDLInfo[dataType].URLHost = purl.Host
	}

	di.MDLInfo[dataType].DownloadURL = dlURL
}

func (di *DownloadInfo) GetDownloadUrlHost(dataType string) string {
	di.MDLInfo[dataType].RLock()
	defer di.MDLInfo[dataType].RUnlock()
	return di.MDLInfo[dataType].URLHost
}

func (di *DownloadInfo) GetBaseFilePath(dataType string) string {
	di.MDLInfo[dataType].RLock()
	defer di.MDLInfo[dataType].RUnlock()
	return di.MDLInfo[dataType].BasePath
}

func (di *DownloadInfo) SetBaseFilePath(dataType, fpath string) {
	di.MDLInfo[dataType].Lock()
	defer di.MDLInfo[dataType].Unlock()
	di.MDLInfo[dataType].BasePath = fpath
}

func (di *DownloadInfo) SetFinished(dataType string) {
	di.MDLInfo[dataType].Lock()
	defer di.MDLInfo[dataType].Unlock()
	di.MDLInfo[dataType].Finished = true
}

func (di *DownloadInfo) IsFinished(dataType string) bool {
	di.MDLInfo[dataType].RLock()
	defer di.MDLInfo[dataType].RUnlock()
	return di.MDLInfo[dataType].Finished
}

func (di *DownloadInfo) GetTimeSinceUpdated() time.Duration {
	di.RLock()
	defer di.RUnlock()
	return time.Since(di.LastUpdated)
}

func (fi FormatInfo) SetInfo(player_response *PlayerResponse) {
	pmfr := player_response.Microformat.PlayerMicroformatRenderer
	vid := player_response.VideoDetails.VideoID
	startDate := strings.ReplaceAll(pmfr.LiveBroadcastDetails.StartTimestamp, "-", "")
	publishDate := strings.ReplaceAll(pmfr.PublishDate, "-", "")
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", vid)

	if len(startDate) > 0 {
		startDate = startDate[:8]
	}

	fi["id"] = vid
	fi["url"] = url
	fi["title"] = strings.TrimSpace(player_response.VideoDetails.Title)
	fi["channel_id"] = player_response.VideoDetails.ChannelID
	fi["channel"] = player_response.VideoDetails.Author
	fi["upload_date"] = startDate
	fi["start_date"] = startDate
	fi["publish_date"] = publishDate
	fi["description"] = strings.TrimSpace(player_response.VideoDetails.ShortDescription)
}

func (mi MetaInfo) SetInfo(fi FormatInfo) {
	for k, v := range mi {
		val, err := FormatPythonMapString(v, fi)
		if err != nil {
			// ignore and just leave unformatted
			continue
		}

		mi[k] = val
	}
}

func (di *DownloadInfo) printChannelAndTitle(pr *PlayerResponse) {
	if di.InfoPrinted {
		return
	}

	if len(pr.VideoDetails.Title) == 0 || len(pr.VideoDetails.Author) == 0 {
		return
	}

	LogGeneral("Channel: %s\n", pr.VideoDetails.Author)
	LogGeneral("Video Title: %s\n", pr.VideoDetails.Title)
	di.InfoPrinted = true
}

func (di *DownloadInfo) printStatusWithoutLock() {
	if loglevel >= LoglevelError {
		fmt.Print(di.Status)
	}
}

func (di *DownloadInfo) SetStatus(status string) {
	di.Lock()
	defer di.Unlock()
	di.Status = status
	di.printStatusWithoutLock()
}

func (di *DownloadInfo) PrintStatus() {
	di.RLock()
	defer di.RUnlock()

	di.printStatusWithoutLock()
}

func (di *DownloadInfo) SaveState(itag int) {
	if di.DisableSaveState || len(di.DLState[itag].File) == 0 {
		return
	}

	data, err := json.Marshal(di.DLState[itag])
	if err != nil {
		LogWarn("Error when saving state: %s", err)
		return
	}

	err = os.WriteFile(di.DLState[itag].File, data, di.FileMode)
	if err != nil {
		LogWarn("Error when saving state: %s", err)
		return
	}
}

// Ask if the user wants to wait for a scheduled stream to start and then record it
func (di *DownloadInfo) AskWaitForStream() bool {
	LogGeneral("%s\n%s\n",
		fmt.Sprintf("%s is likely a future scheduled livestream.", di.URL),
		"Would you like to wait for the scheduled start time, poll until it starts, or not wait?",
	)
	choice := strings.ToLower(GetUserInput("wait/poll/[no]: "))

	if strings.HasPrefix(choice, "wait") {
		return true
	} else if strings.HasPrefix(choice, "poll") {
		secs := GetUserInput("Input poll interval in seconds (minimum 15): ")
		s, err := strconv.Atoi(secs)
		if err != nil || s < DefaultPollTime {
			s = DefaultPollTime
		}

		di.RetrySecs = s
		return true
	}

	return false
}

func (di *DownloadInfo) GetGvideoUrl(dataType string) {
	for {
		gvUrl := GetUserInput(fmt.Sprintf("Please enter the %s url, or nothing to skip: ", dataType))
		if len(gvUrl) == 0 {
			return
		}

		newUrl, itag := ParseGvideoUrl(gvUrl, dataType)
		if len(newUrl) == 0 {
			continue
		}

		if dataType == DtypeVideo {
			di.Quality = itag
		}

		if (dataType == DtypeAudio && itag == AudioItag) ||
			(dataType == DtypeVideo && itag != AudioItag) {
			di.SetDownloadUrl(dataType, newUrl)
			break
		} else {
			LogGeneral("URL given does not appear to be appropriate for the data type needed.")
		}
	}
}

func (di *DownloadInfo) ParseLiveFromStrVal() error {
	if di.LiveFromVal == "" {
		return nil
	}

	if strings.ToLower(di.LiveFromVal) == "now" {
		// --live-from now
		//  Seek to current sequence number
		di.LiveFromSq = di.LastSq
		LogGeneral("Starting download from current time")
	} else {
		durationVal := strings.TrimPrefix(di.LiveFromVal, "-") // Removes negative symbol from start of duration string

		// Try to parse the value as a duration string
		duration, err := str2duration.ParseDuration(durationVal)
		if err != nil {
			// Try to parse the value as a HH:MM:SS string
			duration, err = hhmmss.Parse(durationVal)
			if err != nil {
				LogError("Unable to parse value as either a duration or a time string: %v", err)
				return err
			}
		}

		secondsTotal := duration.Seconds()
		fragDur := float64(di.TargetDuration)
		secondsRoundedToFragLength := int(math.Ceil(secondsTotal/fragDur) * fragDur) // Rounds up to next frag interval time
		noOfFragsToJump := secondsRoundedToFragLength / di.TargetDuration

		if strings.HasPrefix(di.LiveFromVal, "-") {
			// --live-from negative value
			//  Seek to a sequence number in the past

			// Invalid time specification (too short or too long)
			if secondsTotal < 0 || secondsTotal > LiveMaximumSeekable {
				LogError("Invalid duration specified '%s'. (Maximum video seek time is %d days)", di.LiveFromVal, (LiveMaximumSeekable / 60 / 60 / 24))
				return errors.New("invalid duration specified")
			}
			// If the stream hasn't been live long enough for the specified duration
			if noOfFragsToJump > di.LastSq {
				streamLength := di.LastSq * di.TargetDuration
				curStreamDuration := SecondsToDurationAndTimeStr(streamLength)

				LogError("Invalid duration specified. The stream has not been live for that long [Live for %s].", curStreamDuration)
				return errors.New("invalid duration specified")
			}

			di.LiveFromSq = di.LastSq - noOfFragsToJump
			LogGeneral("Jumping back %d seconds from now, and starting to download from that time.", secondsRoundedToFragLength)
			LogDebug("Jumping back %d frags. Will start from sequence %d [current is %d].", noOfFragsToJump, di.LiveFromSq, di.LastSq)
		} else {
			// --live-from positive value
			// Calculate the sequence number of the specified stream time to start from.
			maxSq := di.LastSq
			targetStartFrag := noOfFragsToJump

			// Stream hasn't been live long enough
			if di.LastSq < targetStartFrag {
				streamLength := di.LastSq * di.TargetDuration
				curStreamDuration := SecondsToDurationAndTimeStr(streamLength)

				errStr := fmt.Errorf("invalid duration specified. the stream has not been live for that long [live for %s]", curStreamDuration)
				return errors.New(errStr.Error())
			} else {
				// Make sure the Start Frag is within the 5 day limit.
				if targetStartFrag < (di.LastSq - LiveMaximumSeekable) {
					LogError("YT only retains the livestream 5 days past for seeking, your --live-from value of '%s' is not valid.", di.LiveFromVal)

					// Calculate how long the stream has been live for
					streamLiveTime := di.LastSq * di.TargetDuration
					minSeekTime := streamLiveTime - LiveMaximumSeekable
					LogError("You must specify a --live-from value between: %s and %s", SecondsToDurationAndTimeStr(minSeekTime), SecondsToDurationAndTimeStr(streamLiveTime))
					return errors.New("value is not valid for stream duration")
				}

				di.LiveFromSq = targetStartFrag
				startTimeStr := SecondsToDurationAndTimeStr(di.LiveFromSq * di.TargetDuration)
				totalTimeToGrabStr := SecondsToDurationAndTimeStr((maxSq - di.LiveFromSq) * di.TargetDuration)
				LogGeneral("Starting from stream time '%s' and grabbing '%s' of content (and counting).", startTimeStr, totalTimeToGrabStr)
				LogDebug("Starting from sequence %d [max right now is %d]", di.LiveFromSq, maxSq)
			}
		}
	}

	return nil
}

func (di *DownloadInfo) ParseInputUrl() error {
	parsedUrl, err := url.Parse(di.URL)
	if err != nil {
		return err
	}

	lowerHost := strings.ToLower(parsedUrl.Host)
	lowerHost = strings.TrimPrefix(lowerHost, "www.")
	lowerHost = strings.TrimPrefix(lowerHost, "m.")
	lowerPath := strings.ToLower(parsedUrl.EscapedPath())
	parsedQuery := parsedUrl.Query()

	if lowerHost == "youtube.com" {
		if strings.HasPrefix(lowerPath, "/watch") {
			if _, ok := parsedQuery["v"]; !ok {
				return errors.New("youtube URL missing video ID")
			}

			di.VideoID = parsedQuery.Get("v")
			return nil
		} else if strings.HasPrefix(lowerPath, "/channel/") ||
			strings.HasPrefix(lowerPath, "/c/") ||
			strings.HasPrefix(lowerPath, "/user/") ||
			strings.HasPrefix(lowerPath, "/@") {
			// The URL can be polled and the stream can change depending on what
			// the channel schedules. Useful for set-and-forget
			chanSlashIdx := strings.Index(lowerPath[1:], "/") + 1
			noChanPath := lowerPath[chanSlashIdx:]

			// Check if we were given the channel url on a sub page
			// Remove that part from the URL so we can append /live to it after
			if strings.LastIndex(noChanPath, "/") > 0 {
				lastSlash := strings.LastIndex(di.URL, "/")
				di.URL = di.URL[:lastSlash]
			}

			di.URL = fmt.Sprintf("%s/live", di.URL)
			di.LiveURL = true
			return nil
		} else if strings.HasPrefix(lowerPath, "/live/") {
			di.VideoID = strings.TrimPrefix(parsedUrl.EscapedPath(), "/live/")
			return nil
		} else if strings.HasPrefix(lowerPath, "/shorts/") {
			di.VideoID = strings.TrimPrefix(parsedUrl.EscapedPath(), "/shorts/")
			return nil
		}
	} else if lowerHost == "youtu.be" {
		di.VideoID = strings.TrimLeft(parsedUrl.EscapedPath(), "/")
		return nil
	} else if strings.HasSuffix(lowerHost, ".googlevideo.com") {
		if _, ok := parsedQuery["noclen"]; !ok {
			return errors.New("given Google Video URL is not for a fragmented stream")
		}

		di.GVideoDDL = true
		id := parsedQuery.Get("id")
		dotIdx := strings.LastIndex(id, ".")
		id = id[:dotIdx]
		di.VideoID = id
		di.FormatInfo["id"] = di.VideoID
		sqIdx := strings.Index(di.URL, "&sq=")
		itag, err := strconv.Atoi(parsedQuery.Get("itag"))

		if err != nil {
			return fmt.Errorf("error parsing itag parameter of Google Video URL: %s", err)
		}

		if sqIdx < 0 {
			return errors.New("could not find 'sq' parameter in given Google Video URL")
		}

		if itag == AudioItag {
			if len(di.GetDownloadUrl(DtypeAudio)) == 0 {
				di.SetDownloadUrl(DtypeAudio, di.URL[:sqIdx]+"&sq=%d")
			}

			if len(di.GetDownloadUrl(DtypeVideo)) == 0 && !di.AudioOnly {
				di.GetGvideoUrl(DtypeVideo)
			}
		} else {
			if len(di.GetDownloadUrl(DtypeVideo)) == 0 {
				di.SetDownloadUrl(DtypeVideo, di.URL[:sqIdx]+"&sq=%d")
			}

			if len(di.GetDownloadUrl(DtypeAudio)) == 0 && !di.VideoOnly {
				di.GetGvideoUrl(DtypeAudio)
			}
		}

		di.Quality = itag
		return nil
	}

	return fmt.Errorf("%s is not a known valid youtube URL", di.URL)
}

/*
Get download URLs either from the DASH manifest or from the adaptiveFormats.
Prioritize DASH manifest if it is available.
Attempts to grab from the Web API player response as well as desktop,
favouring Web API. Any formats not found in the Web API are looked for in the
desktop player response.
*/
func (di *DownloadInfo) GetDownloadUrls(pr *PlayerResponse) map[int]string {
	urls := make(map[int]string)
	WebPlayerResponse, err := di.DownloadWebPlayerResponse()

	if err != nil {
		LogDebug("Error getting Web API player response: %s", err.Error())
	} else {
		if len(WebPlayerResponse.StreamingData.DashManifestURL) > 0 {
			LogDebug("Retrieving URLs from Web API DASH manifest")
			manifest := DownloadData(WebPlayerResponse.StreamingData.DashManifestURL)
			if len(manifest) > 0 {
				// we store the LastSq to calculate 5 days past
				urls, di.LastSq = GetUrlsFromManifest(manifest, di.PoToken)
			}

			for itag := range urls {
				LogTrace("Setting itag %d from Web API DASH manifest", itag)
			}
		}

		if len(WebPlayerResponse.StreamingData.AdaptiveFormats) > 0 {
			LogDebug("Retrieving URLs from Web API adaptive formats")
			for _, fmt := range WebPlayerResponse.StreamingData.AdaptiveFormats {
				if len(fmt.URL) == 0 {
					continue
				}
				if _, ok := urls[fmt.Itag]; ok { // format exists already
					continue
				}

				urls[fmt.Itag] = strings.ReplaceAll(fmt.URL, "%", "%%") + "&sq=%d"
				LogTrace("Setting itag %d from Web API adaptive formats", fmt.Itag)
			}
		}
	}

	if len(pr.StreamingData.DashManifestURL) > 0 {
		LogDebug("Retrieving URLs from web DASH manifest")
		manifest := DownloadData(pr.StreamingData.DashManifestURL)
		if len(manifest) > 0 {
			// we store the LastSq to calculate 5 days past
			dashUrls, lastSq := GetUrlsFromManifest(manifest, di.PoToken)
			if lastSq > di.LastSq {
				di.LastSq = lastSq
			}

			for itag, url := range dashUrls {
				if _, ok := urls[itag]; ok { // format exists already
					continue
				}

				urls[itag] = url
				LogTrace("Setting itag %d from web adaptive formats", itag)
			}
		}
	}

	if len(pr.StreamingData.AdaptiveFormats) > 0 {
		LogDebug("Retrieving URLs from web adaptive formats")
		for _, fmt := range pr.StreamingData.AdaptiveFormats {
			if len(fmt.URL) == 0 {
				continue
			}
			if _, ok := urls[fmt.Itag]; ok { // format exists already
				continue
			}

			urls[fmt.Itag] = strings.ReplaceAll(fmt.URL, "%", "%%") + "&sq=%d"
			LogTrace("Setting itag %d from web adaptive formats", fmt.Itag)
		}
	}

	return urls
}

func (di *DownloadInfo) ParseCaptureDurationStrVal(durationVal string) error {
	if durationVal == "" {
		return nil
	}

	// Try to parse the value as a duration string
	duration, err := str2duration.ParseDuration(durationVal)
	if err != nil {
		// Try to parse the value as a HH:MM:SS string
		duration, err = hhmmss.Parse(durationVal)
		if err != nil {
			LogError("Unable to parse value as either a Duration or a Time String: %v", err)
			return errors.New("invalid duration string")
		}
	}

	di.CaptureDurationSecs = int(duration.Seconds())
	return nil
}

func (di *DownloadInfo) ParseStartDelayStrVal(durationVal string) error {
	if durationVal == "" {
		return nil
	}

	// Try to parse the value as a duration string
	duration, err := str2duration.ParseDuration(durationVal)
	if err != nil {
		// Try to parse the value as a HH:MM:SS string
		duration, err = hhmmss.Parse(durationVal)
		if err != nil {
			LogError("Unable to parse value as either a Duration or a Time String: %v", err)
			return errors.New("invalid duration string")
		}
	}

	di.StartDelaySecs = int(duration.Seconds())
	return nil
}

// Get necessary video info such as video/audio URLs
func (di *DownloadInfo) GetVideoInfo() bool {
	di.Lock()
	defer di.Unlock()

	/*
		No point retrieving information if we know it's not available, or there
		is nothing useful to be gotten
	*/
	if di.GVideoDDL || di.Stopping || di.Unavailable {
		return false
	}

	// Almost nothing we care about is likely to change in 15 seconds
	delta := time.Since(di.LastUpdated)
	if delta < (DefaultPollTime * time.Second) {
		return false
	}

	retrieved, pr, selQaulities := di.GetPlayablePlayerResponse()
	di.LastUpdated = time.Now()
	if retrieved == PlayerResponseNotFound {
		di.Live = false
		di.Unavailable = true
		return false
	} else if retrieved == PlayerResponseNotUsable {
		return false
	}

	streamData := pr.StreamingData
	pmfr := pr.Microformat.PlayerMicroformatRenderer
	isLive := pmfr.LiveBroadcastDetails.IsLiveNow

	targetDur := int(streamData.AdaptiveFormats[0].TargetDurationSec)
	if targetDur > 0 {
		di.TargetDuration = targetDur
	}
	dlUrls := di.GetDownloadUrls(pr)

	if len(dlUrls) == 0 {
		LogError("No download URLs found")
		return false
	}

	if di.Quality < 0 {
		var qualities []string
		qualities = append(qualities, "audio_only")
		found := false

		for _, qlabel := range VideoQualities {
			videoItag := VideoLabelItags[qlabel]
			_, vp9Ok := dlUrls[videoItag.VP9]
			_, h264Ok := dlUrls[videoItag.H264]

			if Contains(qualities, qlabel) || (!vp9Ok && !h264Ok) {
				continue
			}
			qualities = append(qualities, qlabel)
		}

		for !found {
			if len(selQaulities) == 0 {
				selQaulities = GetQualityFromUser(qualities, false)
			}

			for _, q := range selQaulities {
				q = strings.TrimSpace(q)

				if q == "best" {
					q = qualities[len(qualities)-1]
				} else if q == "audio" {
					q = "audio_only"
				}

				videoItag := VideoLabelItags[q]
				aonly := videoItag.VP9 == AudioOnlyQuality

				if !di.VideoOnly {
					di.SetDownloadUrl(DtypeAudio, dlUrls[AudioItag])
				}

				if aonly {
					di.Quality = AudioOnlyQuality
					di.SetDownloadUrl(DtypeVideo, "")
					found = true
					break
				}

				_, vp9Ok := dlUrls[videoItag.VP9]
				_, h264Ok := dlUrls[videoItag.H264]

				if vp9Ok && (di.VP9 || !h264Ok) && !di.H264 { // Sometimes a quality is VP9 only apparently
					di.SetDownloadUrl(DtypeVideo, dlUrls[videoItag.VP9])
					di.Quality = videoItag.VP9
					found = true
					LogGeneral("Selected quality: %s (VP9)\n", q)
					break
				} else if h264Ok {
					di.SetDownloadUrl(DtypeVideo, dlUrls[videoItag.H264])
					di.Quality = videoItag.H264
					found = true
					LogGeneral("Selected quality: %s (h264)\n", q)
					break
				}
			}

			/*
				None of the qualities the user gave were available
				Should only be possible if they chose to wait for a stream
				and chose only qualities that the streamer ended up not using
				i.e. 1080p60/720p60 when the stream is only available in 30 FPS
			*/
			if !found {
				LogGeneral("The qualities you selected ended up unavailable for this stream")
				LogGeneral("You will now have the option to select from the available qualities")
				selQaulities = selQaulities[len(selQaulities):]
			}
		}
	} else {
		aonly := di.Quality == AudioOnlyQuality
		_, audioOk := dlUrls[AudioItag]

		if !di.VideoOnly && audioOk && IsFragmented(dlUrls[AudioItag]) {
			di.SetDownloadUrl(DtypeAudio, dlUrls[AudioItag])
		}

		if !aonly {
			_, vidOk := dlUrls[di.Quality]
			if vidOk && IsFragmented(dlUrls[di.Quality]) {
				di.SetDownloadUrl(DtypeVideo, dlUrls[di.Quality])
			}
		}
	}

	if !di.InProgress {
		LogGeneral("Stream started at time %s", pmfr.LiveBroadcastDetails.StartTimestamp)
		di.FormatInfo.SetInfo(pr)
		di.Metadata.SetInfo(di.FormatInfo)
		if len(pmfr.Thumbnail.Thumbnails) > 0 {
			di.Thumbnail = pmfr.Thumbnail.Thumbnails[0].URL
		}
		di.InProgress = true
	}

	di.Live = isLive

	return true
}

func (di *DownloadInfo) WaitForStartDelay() bool {
	if di.Live && di.StartDelaySecs > 0 {
		fragDur := float64(di.TargetDuration)
		secondsRoundedToFragLength := int(math.Ceil(float64(di.StartDelaySecs)/fragDur) * fragDur) // Rounds up to next frag interval time
		noOfFragsToSkip := secondsRoundedToFragLength / di.TargetDuration
		di.LiveFromSq = di.LastSq + noOfFragsToSkip

		LogGeneral("Waiting %s before starting to download...", SecondsToDurationAndTimeStr(secondsRoundedToFragLength))
		LogDebug("Will start from sequence %d [current is %d]", di.LiveFromSq, di.LastSq)

		time.Sleep(time.Duration(secondsRoundedToFragLength) * time.Second) // Waits for the specified length of time.

		if secondsRoundedToFragLength > DefaultPollTime {
			return di.GetVideoInfo() // Re-grab video information.
		}
	}

	return true
}

func (di *DownloadInfo) downloadFragment(state *fragThreadState, dataChan chan<- *Fragment) {
	state.Tries = 0
	state.FullRetries = 3
	state.Is403 = false
	fname := fmt.Sprintf("%s.frag%d.ts", state.BaseFilePath, state.SeqNum)

	for state.Tries < int(di.FragMaxTries) || di.FragMaxTries == 0 {
		if di.IsStopping() {
			return
		}
		if di.FragMaxTries == 0 {
			state.Tries = 0 // just in case someone actually somehow lets something run long enough to cause an overflow
		}

		baseUrl := di.GetDownloadUrl(state.DataType)
		seqUrl := fmt.Sprintf(baseUrl, state.SeqNum)

		req, err := http.NewRequest("GET", seqUrl, nil)
		if err != nil {
			LogDebug("%s: error creating request: %s", state.Name, err.Error())
		}

		var resp *http.Response
		dlStart := time.Now()

		if req != nil {
			host := di.GetDownloadUrlHost(state.DataType)
			if len(host) > 0 {
				req.Header.Add("Host", host)
				req.Header.Add("Referer", fmt.Sprintf("https://%s/", host))
			}

			req.Header.Add("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:87.0) Gecko/20100101 Firefox/87.0")
			req.Header.Add("Origin", "https://www.youtube.com")

			resp, err = client.Do(req)
		} else {
			resp, err = client.Get(seqUrl)
		}

		if err != nil {
			HandleFragDownloadError(di, state, err)

			state.Tries += 1
			if !ContinueFragmentDownload(di, state) {
				return
			}

			time.Sleep(state.SleepTime)
			continue
		}

		respData, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		dlDuration := time.Since(dlStart)

		if err != nil {
			HandleFragDownloadError(di, state, err)

			state.Tries += 1
			if !ContinueFragmentDownload(di, state) {
				return
			}

			time.Sleep(state.SleepTime)
			continue
		}

		if resp.StatusCode >= 400 {
			HandleFragHttpError(di, state, resp.StatusCode, baseUrl)

			state.Tries += 1
			if !ContinueFragmentDownload(di, state) {
				return
			}

			time.Sleep(state.SleepTime)
			continue
		}

		/*
			The request was a success but no data was given
			Increment the try counter and wait
		*/
		if len(respData) == 0 {
			state.Tries += 1
			if !ContinueFragmentDownload(di, state) {
				return
			}

			time.Sleep(state.SleepTime)
			continue
		}

		var data *bytes.Buffer
		headerSeqnum := -1
		headerSeqnumStr := resp.Header.Get("X-Head-Seqnum")

		if len(headerSeqnumStr) > 0 {
			headerSeqnum, _ = strconv.Atoi(headerSeqnumStr)
		}

		mimeType := resp.Header.Get("Content-Type")
		if !strings.HasSuffix(mimeType, "/mp4") && !strings.HasSuffix(mimeType, "/webm") {
			LogTrace("%s: fragment %d has unknown MIME type '%s'", state.Name, state.SeqNum, mimeType)
		}

		if state.ToFile {
			err = os.WriteFile(fname, respData, di.FileMode)
			if err != nil {
				LogDebug("%s: Failed to write fragment %d to file: %s", state.Name, state.SeqNum, err)
				di.PrintStatus()

				state.Tries += 1
				if !ContinueFragmentDownload(di, state) {
					TryDelete(fname)
					return
				}

				time.Sleep(state.SleepTime)
				continue
			}
		} else {
			data = bytes.NewBuffer(respData)
		}

		// Fragment took more than 1.5x its length to download and is not that close to the current max seq
		isSlow := false
		if headerSeqnum < 0 || state.SeqNum < (headerSeqnum-10) {
			isSlow = dlDuration > (time.Duration(float64(di.TargetDuration)*1.5) * time.Second)
		}

		dataChan <- &Fragment{
			Seq:         state.SeqNum,
			XHeadSeqNum: headerSeqnum,
			FileName:    fname,
			Data:        data,
			Slow:        isSlow,
			MimeType:    mimeType,
		}

		return
	}
}

func (di *DownloadInfo) DownloadFrags(dataType string, seqChan <-chan *seqChanInfo, dataChan chan<- *Fragment, name string) {
	defer di.DecrementJobs(dataType)
	state := NewFragThreadState(
		name,
		di.GetBaseFilePath(dataType),
		dataType,
		di.FragFiles,
		time.Duration(di.TargetDuration)*time.Second,
	)

	var endSeq int // End seq to stop on for the --capture-duration option.
	for seqInfo := range seqChan {
		if di.IsStopping() || di.IsFinished(dataType) {
			break
		}

		// --capture-duration: Stop if reaching the maximum DurationSecs.
		if di.CaptureDurationSecs != 0 {
			if endSeq == 0 {
				capSeqCnt := int(math.Ceil(float64(di.CaptureDurationSecs) / float64(di.TargetDuration)))
				endSeq = seqInfo.CurSequence + capSeqCnt // Calculate ending seq based on current seq number and DurationSecs.
			} else {
				if seqInfo.CurSequence >= endSeq {
					LogDebug("%s: Reached the maximum duration specified by --capture-duration.", name)
					di.SetFinished(dataType)
					break
				}
			}
		}

		if seqInfo.MaxSequence > -1 && !di.IsLive() && seqInfo.CurSequence >= seqInfo.MaxSequence {
			LogDebug("%s: Stream is finished and highest sequence reached", name)
			di.SetFinished(dataType)
			break
		}

		state.SeqNum = seqInfo.CurSequence
		state.MaxSeq = seqInfo.MaxSequence

		di.downloadFragment(state, dataChan)
	}

	LogDebug("%s: exiting", name)
	di.PrintStatus()
}

func (di *DownloadInfo) DownloadStream(dataType, dataFile string, progressChan chan<- *ProgressInfo, done chan<- struct{}) {
	dataChan := make(chan *Fragment, di.Jobs*2)
	seqChan := make(chan *seqChanInfo, di.Jobs*2)
	closed := false
	curFrag := 0
	startFrag := 0
	activeDownloads := 0
	maxSeqs := -1
	tries := 10
	jobNum := 1
	slowFrags := 0
	lastSlowFrag := 0
	itag := 0
	dataToWrite := make([]*Fragment, 0, di.Jobs)
	deletingFrags := make([]string, 0, 1)
	logName := fmt.Sprintf("%s-download", dataType)
	var f *os.File
	var err error
	defer func() { done <- struct{}{} }()

	if dataType == DtypeAudio {
		itag = AudioItag
	} else {
		itag = di.Quality
	}

	var resumedState bool = false
	if di.DLState[itag].Fragments > 0 {
		if di.LiveFromSq != 0 {
			if di.LiveFromVal != "" {
				LogWarn("%s: Option --live-from is being ignored as a download is being resumed.", dataType)
			}
			if di.StartDelaySecs != 0 {
				LogWarn("%s: Option --start-delay is being ignored as a download is being resumed.", dataType)
			}
		}

		f, err = os.OpenFile(dataFile, os.O_RDWR, 0666)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				LogWarn("%s: Failed to open %s to resume download: %s", dataType, dataFile, err)
				LogWarn("%s: Will truncate and start from the beginning", dataType)
			} else {
				resumedState = true
			}
			f, err = os.Create(dataFile)
		} else {
			_, err = f.Seek(di.DLState[itag].Size, 0)
			if err != nil {
				LogWarn("%s: Failed to seek %s to resume download: %s", dataType, dataFile, err)
				LogWarn("%s: Will truncate and start from the beginning", dataType)
				f, err = os.Create(dataFile)
			} else {
				resumedState = true
			}
		}
	} else {
		f, err = os.Create(dataFile)
	}

	if resumedState {
		// Resumed state: Set the startFrag and curFrag values from the state file.
		startFrag = di.DLState[itag].StartFrag
		curFrag = startFrag + di.DLState[itag].Fragments
		maxSeqs = di.LastSq
		LogInfo("%s: Resuming download from sequence %d", dataType, curFrag)
	} else {
		if di.LastSq >= 0 {
			curFrag = di.LastSq - (LiveMaximumSeekable / (di.TargetDuration))
			maxSeqs = di.LastSq
		}

		if di.LiveFromSq != 0 {
			// --live-from or --start-delay: Set start sequence.
			curFrag = di.LiveFromSq
			startFrag = curFrag

			if di.LiveFromVal != "" {
				LogDebug("%s: Starting from sequence %d (latest is %d)", dataType, startFrag, di.LastSq)
			}
			if di.StartDelaySecs != 0 {
				LogDebug("%s: Starting from sequence %d (latest is %d)", dataType, startFrag, di.LastSq)
			}
		} else if curFrag > 0 {
			// Stream that has been live for more than 5 days.
			LogWarn("%s: YT only retains the livestream 5 days past for seeking, starting from sequence %d (latest is %d)", dataType, curFrag, di.LastSq)
			startFrag = curFrag
		} else {
			// All other stream lengths.
			curFrag = 0
		}

		di.DLState[itag].StartFrag = startFrag // Sets start frag in state file for resuming.
	}
	curSeq := curFrag

	if err != nil {
		LogError("%s: Error opening %s for writing: %s", dataType, dataFile, err)
		di.Stop()
		return
	}
	defer f.Close()

	for di.GetActiveJobCount(dataType) < di.Jobs {
		jobName := fmt.Sprintf("%s%d", dataType, jobNum)
		di.IncrementJobs(dataType)
		seqChan <- &seqChanInfo{curSeq, maxSeqs}
		curSeq += 1
		activeDownloads += 1
		jobNum += 1
		go di.DownloadFrags(dataType, seqChan, dataChan, jobName)
	}

	for {
		dataReceived := false
		downloading := di.GetActiveJobCount(dataType) > 0
		stopping := di.IsStopping()

		if stopping || !downloading || di.IsFinished(dataType) {
			if !closed {
				close(seqChan)
				closed = true
			}
		} else if slowFrags >= 10 {
			RefreshURL(di, dataType, "")
			slowFrags = 0
		}

	getData:
		for {
			select {
			case data := <-dataChan:
				dataReceived = true
				dataToWrite = append(dataToWrite, data)
				activeDownloads -= 1

				if !downloading || stopping || closed {
					continue
				}

				if data.XHeadSeqNum > maxSeqs {
					maxSeqs = data.XHeadSeqNum
				}

				if maxSeqs > 0 {
					for (curSeq <= maxSeqs+1 && activeDownloads < di.Jobs) || activeDownloads < 1 {
						seqChan <- &seqChanInfo{curSeq, maxSeqs}
						curSeq += 1
						activeDownloads += 1
					}
				} else {
					seqChan <- &seqChanInfo{curSeq, maxSeqs}
					curSeq += 1
					activeDownloads += 1
				}

				if data.Slow {
					// Only increment if it's been less than 10 frags since the last slow one
					// Reset the counter otherwise. Should hopefully prevent getting rid of
					// an otherwise good download url
					if (data.Seq - lastSlowFrag) < 10 {
						slowFrags += 1
					} else {
						slowFrags = 1
					}

					lastSlowFrag = data.Seq
				}
			default:
				break getData
			}
		}

		if (len(dataToWrite) == 0 || !dataReceived) && downloading {
			if !stopping && activeDownloads <= 0 {
				LogDebug("%s: Somehow no active downloads and no data to write", logName)
				LogDebug("%s: Fragment this happened at: %d", logName, curFrag)
				di.PrintStatus()

				for activeDownloads < di.GetActiveJobCount(dataType) {
					seqChan <- &seqChanInfo{curSeq, maxSeqs}
					curSeq += 1
					activeDownloads += 1
				}
			}

			time.Sleep(100 * time.Millisecond)
			continue
		}

		i := 0
		for i < len(dataToWrite) && tries > 0 {
			data := dataToWrite[i]
			if data.Seq != curFrag {
				i += 1
				continue
			}

			if di.FragFiles {
				readBytes, err := os.ReadFile(data.FileName)

				if err != nil {
					tries -= 1
					LogWarn("%s: Error when attempting to read fragment %d for writing: %s", logName, curFrag, err)
					di.PrintStatus()

					if tries > 0 {
						LogWarn("%s: Will try %d more time(s)", logName, tries)
						di.PrintStatus()
					}

					continue
				}

				data.Data = bytes.NewBuffer(readBytes)
			}

			bytesWritten := 0
			buf := make([]byte, BufferSize)

			rc, _ := data.Data.Read(buf)

			writeBuf := buf
			// ffmpeg doesn't like certain atoms in concatenated MP4 files, so we remove those here
			// If MimeType is blank, assume MP4
			if strings.HasSuffix(data.MimeType, "/mp4") || data.MimeType == "" {
				badAtoms := []string{"sidx"}
				// ffmpeg 6.1 doesn't like multiple ftyp atoms, so only allow on the first fragment
				if curFrag != startFrag {
					badAtoms = append(badAtoms, "ftyp")
				}
				writeBuf = RemoveAtoms(buf[:rc], badAtoms...)
			}

			count, err := f.Write(writeBuf)
			bytesWritten += count

			if err != nil {
				tries -= 1
				LogWarn("%s: Error when attempting to write fragment %d to %s: %s", logName, curFrag, dataFile, err)
				di.PrintStatus()

				// If we errored but wrote some data, set the offset back to
				// where we want to write the fragment
				f.Seek(int64(bytesWritten), 1)

				if tries > 0 {
					LogWarn("%s: Will try %d more time(s)", logName, tries)
					di.PrintStatus()
				}

				continue
			}

			for {
				count, err = data.Data.Read(buf)
				if err != nil {
					break
				}

				count, err = f.Write(buf[:count])
				bytesWritten += count

				if err != nil {
					tries -= 1
					LogWarn("%s: Error when attempting to write fragment %d to %s: %s", logName, curFrag, dataFile, err)
					di.PrintStatus()

					f.Seek(int64(bytesWritten), 1)

					if tries > 0 {
						LogWarn("%s: Will try %d more time(s)", logName, tries)
						di.PrintStatus()
					}

					break
				}
			}

			// something didn't work
			if err != nil && err != io.EOF {
				continue
			}

			curFrag += 1
			progressChan <- &ProgressInfo{itag, bytesWritten, maxSeqs, startFrag}

			if di.FragFiles {
				err = os.Remove(data.FileName)
				if err != nil {
					LogWarn("%s: Error deleting fragment %d: %s", logName, data.Seq, err)
					LogWarn("%s: Will try again after the download has finished", logName)
					deletingFrags = append(deletingFrags, data.FileName)
					di.PrintStatus()
				}
			}

			dataToWrite = append(dataToWrite[:i], dataToWrite[i+1:]...)
			tries = 10
			i = 0
		}

		if !downloading {
			break
		}

		updateDelta := di.GetTimeSinceUpdated()
		if !stopping && !di.IsUnavailable() && updateDelta > time.Hour {
			di.GetVideoInfo()
		}

		if tries <= 0 {
			LogWarn("%s: Stopping download, something must be wrong...", logName)
			di.PrintStatus()
			di.Stop()
		}
	}

	if di.FragFiles {
		for _, d := range dataToWrite {
			TryDelete(d.FileName)
		}
	}

	for _, d := range deletingFrags {
		LogInfo("%s: Attempting to delete fragments that failed to be deleted before", logName)
		TryDelete(d)
	}

	LogDebug("%s thread closing", logName)
	di.PrintStatus()
}
