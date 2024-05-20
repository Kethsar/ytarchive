package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
)

type MPD struct {
	Representations []Representation `xml:"Period>AdaptationSet>Representation"`
}

// DASH Manifest element containing Youtube's media ID and a download URL
type Representation struct {
	Id      string `xml:"id,attr"`
	BaseURL string

	// we need the last sq value of the format
	SegmentList []MpdSegments `xml:"SegmentList>SegmentURL"`
}

type MpdSegments struct {
	Media string `xml:"media,attr"`
}

type MpdDuration struct {
	D string `xml:"d,attr"`
}

type Atom struct {
	Offset int
	Length int
}

type FFMpegArgs struct {
	Args     []string
	FileName string
}

const (
	LoglevelQuiet = iota
	LoglevelError
	LoglevelWarning
	LoglevelInfo
	LoglevelDebug
	LoglevelTrace
)

const (
	_           = iota
	KiB float64 = 1 << (10 * iota)
	MiB
	GiB
)

const (
	NetworkBoth         = "tcp"
	NetworkIPv4         = "tcp4"
	NetworkIPv6         = "tcp6"
	DefaultPollTime     = 15
	MinimumMonitorTime  = 30
	DefaultMonitorTime  = 60
	DefaultVideoQuality = "best"
)

// If we run into file length issues, chances are the max file name length is around 255 bytes.
// Seems Go automatically converts to long paths for Windows so we only have to worry about the
// actual file name.
const MaxFileNameLength = 243 // 255 - len(".description")

var (
	HtmlVideoLinkTag = []byte(`<link rel="canonical" href="https://www.youtube.com/watch?v=`)

	loglevel              = LoglevelWarning
	networkType           = NetworkBoth // Set to force IPv4 or IPv6
	networkOverrideDialer = &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	client *http.Client
)

var fnameReplacer = strings.NewReplacer(
    "<", "＜",
    ">", "＞",
    ":", "：",
    `"`, "″",
    "/", "⧸",
    "\\", "⧹",
    "|", "｜",
    "?", "？",
    "*", "＊",
)

/*
Logging functions;
ansi sgr 0=reset, 1=bold, while 3x sets the foreground color:
0black 1red 2green 3yellow 4blue 5magenta 6cyan 7white
*/
func LogGeneral(format string, args ...interface{}) {
	if loglevel >= LoglevelError {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}
		log.Print(msg)
	}
}

func LogError(format string, args ...interface{}) {
	if loglevel >= LoglevelError {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}
		log.Printf("ERROR: \033[31m%s\033[0m\033[K", msg)
	}
}

func LogWarn(format string, args ...interface{}) {
	if loglevel >= LoglevelWarning {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}
		log.Printf("WARNING: \033[33m%s\033[0m\033[K", msg)
	}
}

func LogInfo(format string, args ...interface{}) {
	if loglevel >= LoglevelInfo {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}
		log.Printf("INFO: \033[32m%s\033[0m\033[K", msg)
	}
}

func LogDebug(format string, args ...interface{}) {
	if loglevel >= LoglevelDebug {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}
		log.Printf("DEBUG: \033[36m%s\033[0m\033[K", msg)
	}
}

func LogTrace(format string, args ...interface{}) {
	if loglevel >= LoglevelTrace {
		msg := format
		if len(args) > 0 {
			msg = fmt.Sprintf(format, args...)
		}
		log.Printf("TRACE: \033[35m%s\033[0m\033[K", msg)
	}
}

func DialContextOverride(ctx context.Context, network, addr string) (net.Conn, error) {
	return networkOverrideDialer.DialContext(ctx, networkType, addr)
}

func InitializeHttpClient(proxyUrl *url.URL) {
	tr := http.DefaultTransport.(*http.Transport).Clone()

	tr.DialContext = DialContextOverride
	tr.ResponseHeaderTimeout = 10 * time.Second
	if proxyUrl != nil {
		// Override ProxyFromEnvironment (default setting)
		tr.Proxy = http.ProxyURL(proxyUrl)
	}

	client = &http.Client{
		Transport: tr,
	}
}

// Remove any illegal filename chars
func SterilizeFilename(s string) string {
	return fnameReplacer.Replace(s)
}

// Pretty formatting of byte count
func FormatSize(bsize int64) string {
	bsFloat := float64(bsize)

	switch {
	case bsFloat >= GiB:
		return fmt.Sprintf("%.2fGiB", bsFloat/GiB)
	case bsFloat >= MiB:
		return fmt.Sprintf("%.2fMiB", bsFloat/MiB)
	case bsFloat >= KiB:
		return fmt.Sprintf("%.2fKiB", bsFloat/KiB)
	}
	return fmt.Sprintf("%dB", bsize)
}

/*
This is pretty dumb but the only way to handle sigint in a custom way
Thankfully we don't call this often enough to really care
*/
func getInput(c chan<- string) {
	var input string
	scanner := bufio.NewScanner(os.Stdin)

	if scanner.Scan() {
		input = strings.TrimSpace(scanner.Text())
	}

	c <- input
}

func GetUserInput(prompt string) string {
	var input string
	inputChan := make(chan string)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	defer signal.Reset(os.Interrupt)

	fmt.Print(prompt)
	go getInput(inputChan)

	select {
	case input = <-inputChan:
	case <-sigChan:
		fmt.Println("\nExiting...")
		Exit(1)
	}

	return input
}

func GetYesNo(prompt string) bool {
	yesno := GetUserInput(fmt.Sprintf("%s [y/N]: ", prompt))
	yesno = strings.ToLower(yesno)

	return strings.HasPrefix(yesno, "y")
}

/*
Execute an external process using the given args
Returns the process return code, or -1 on unknown error
*/
func Execute(prog string, args []string) int {
	retcode := 0
	cmd := exec.Command(prog, args...)

	// Allow for binaries in the current working directory
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		LogError(err.Error())
		return -1
	}

	LogDebug("Executing command: %s %s", prog, shellescape.QuoteCommand(cmd.Args))

	err = cmd.Start()
	if err != nil {
		LogError(err.Error())
		return -1
	}

	stderrBuf := make([]byte, 2048)
	for {
		bytes, err := stderr.Read(stderrBuf)
		fmt.Fprint(os.Stderr, string(stderrBuf[:bytes]))

		if err != nil {
			if err != io.EOF {
				LogError(err.Error())
			}

			break
		}
	}

	err = cmd.Wait()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			retcode = cmd.ProcessState.ExitCode()
		} else {
			retcode = -1
			LogError(err.Error())
		}
	}

	return retcode
}

// Download data from the given URL
func DownloadData(url string) []byte {
	var data []byte
	resp, err := client.Get(url)
	if err != nil {
		LogWarn("Failed to retrieve data from %s: %v", url, err)
		return data
	}
	defer resp.Body.Close()

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		LogWarn("Failed to retrieve data from %s: %v", url, err)
		return data
	}

	return data
}

/*
Download the given url to the given file name.
Obviously meant to be used for thumbnail images.
*/
func DownloadThumbnail(url, fname string, fileMode os.FileMode) bool {
	resp, err := client.Get(url)
	if err != nil {
		LogWarn("Failed to download thumbnail: %v", err)
		return false
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		LogWarn("Failed to download thumbnail: %v", err)
		return false
	}

	err = os.WriteFile(fname, data, fileMode)
	if err != nil {
		LogWarn("Failed to write thumbnail: %v", err)
		os.Remove(fname)
		return false
	}

	return true
}

// Make a comma-separated list of available formats
func MakeQualityList(formats []string) string {
	var sb strings.Builder

	for _, v := range formats {
		fmt.Fprintf(&sb, "%s, ", v)
	}

	sb.WriteString("best")
	return sb.String()
}

// Parse the user-given list of qualities they are willing to accept for download
func ParseQualitySelection(formats []string, quality string) []string {
	var selQualities []string
	quality = strings.ToLower(strings.TrimSpace(quality))
	qualities := strings.Split(quality, "/")

	for _, q := range qualities {
		stripped := strings.TrimSpace(q)

		if stripped == "best" {
			selQualities = append(selQualities, stripped)
			continue
		} else if stripped == "audio" {
			selQualities = append(selQualities, stripped)
			continue
		}

		for _, v := range formats {
			if stripped == v {
				selQualities = append(selQualities, stripped)
				break
			}
		}
	}

	if len(selQualities) < 1 {
		fmt.Println("No valid qualities selected")
	}

	return selQualities
}

// Prompt the user to select a video quality
func GetQualityFromUser(formats []string, waiting bool) []string {
	var selQualities []string
	qualities := MakeQualityList(formats)

	if waiting {
		fmt.Printf("%s\n%s\n%s\n\n",
			"Since you are going to wait for the stream, you must pre-emptively select a video quality.",
			"There is no way to know which qualities will be available before the stream starts, so a list of all possible stream qualities will be presented.",
			"You can use youtube-dl style selection (slash-delimited first to last preference). Default is 'best'",
		)
	}

	fmt.Printf("Available video qualities: %s\n", qualities)

	for len(selQualities) < 1 {
		quality := GetUserInput("Enter desired video quality: ")
		quality = strings.ToLower(quality)
		if len(quality) == 0 {
			quality = DefaultVideoQuality
		}

		selQualities = ParseQualitySelection(formats, quality)
	}

	return selQualities
}

/*
Per anon, there will be a noclen parameter if the given URLs are meant to
be downloaded in fragments. Else it will have a clen parameter, obviously
specifying content length.
*/
func IsFragmented(url string) bool {
	return strings.Index(strings.ToLower(url), "noclen") > 0
}

// Prase the DASH manifest XML and get the download URLs from it
func GetUrlsFromManifest(manifest []byte) (map[int]string, int) {
	urls := make(map[int]string)
	var mpd MPD

	err := xml.Unmarshal(manifest, &mpd)
	if err != nil {
		LogDebug("Error parsing DASH manifest: %s", err)
		return urls, -1
	}

	lastSq := -1

	for _, r := range mpd.Representations {
		itag, err := strconv.Atoi(r.Id)
		if err != nil {
			continue
		}

		sl := r.SegmentList
		if len(sl) > 0 {
			lastMedia := sl[len(sl)-1].Media
			paths := strings.Split(lastMedia, "/")
			for i, ps := range paths {
				if ps == "sq" && len(paths) >= i+1 {
					lastSqC, err := strconv.Atoi(paths[i+1])
					if err != nil {
						lastSqC = -1
					}
					if lastSq < lastSqC {
						lastSq = lastSqC
					}
					break
				}
			}
		}

		if itag > 0 && len(r.BaseURL) > 0 {
			urls[itag] = strings.ReplaceAll(r.BaseURL, "%", "%%") + "sq/%d"
		}
	}

	return urls, lastSq
}

func StringsIndex(arr []string, s string) int {
	for i := 0; i < len(arr); i++ {
		if arr[i] == s {
			return i
		}
	}

	return -1
}

// https://stackoverflow.com/a/61822301
func InsertStringAt(arr []string, idx int, s string) []string {
	if len(arr) == idx {
		return append(arr, s)
	}

	arr = append(arr[:idx+1], arr[idx:]...)
	arr[idx] = s
	return arr
}

func GetAtoms(data []byte) map[string]Atom {
	atoms := make(map[string]Atom)
	ofs := 0

	for {
		if ofs+8 >= len(data) {
			break
		}

		lenHex := hex.EncodeToString(data[ofs : ofs+4])
		aLen, err := strconv.ParseInt(lenHex, 16, 0)

		if err != nil || int(aLen) > len(data) {
			break
		}

		aName := string(data[ofs+4 : ofs+8])
		atoms[aName] = Atom{Offset: ofs, Length: int(aLen)}
		ofs += int(aLen)
	}

	return atoms
}

func RemoveAtoms(data []byte, atomList ...string) []byte {
	atoms := GetAtoms(data)

	var atomsToRemove []Atom
	for _, atomName := range atomList {
		atom, ok := atoms[atomName]
		if !ok {
			continue
		}
		atomsToRemove = append(atomsToRemove, atom)
	}

	// Sort atoms by byte offset in descending order,
	// this lets us remove them in order without affecting the next atom's offset
	sort.Slice(atomsToRemove, func(i, j int) bool {
		return atomsToRemove[i].Offset > atomsToRemove[j].Offset
	})

	for _, atom := range atomsToRemove {
		ofs := atom.Offset
		rlen := atom.Offset + atom.Length
		data = append(data[:ofs], data[rlen:]...)
	}

	return data
}

func GetVideoIdFromWatchPage(data []byte) string {
	startIdx := bytes.Index(data, HtmlVideoLinkTag)
	if startIdx < 0 {
		return ""
	}

	startIdx += len(HtmlVideoLinkTag)
	endIdx := bytes.Index(data[startIdx:], []byte(`"`)) + startIdx

	return string(data[startIdx:endIdx])
}

func ParseGvideoUrl(gvUrl, dataType string) (string, int) {
	var newUrl string
	parsedUrl, err := url.Parse(gvUrl)
	if err != nil {
		LogError("Error parsing Google Video URL: %s", err)
		return newUrl, 0
	}

	gvUrl = strings.ReplaceAll(gvUrl, "%", "%%")
	lowerHost := strings.ToLower(parsedUrl.Hostname())
	sqIndex := strings.Index(gvUrl, "&sq=")

	itag, err := strconv.Atoi(parsedUrl.Query().Get("itag"))
	if err != nil {
		LogError("Error parsing itag in Google Video URL: %s", err)
		return newUrl, 0
	}

	if !strings.HasSuffix(lowerHost, ".googlevideo.com") {
		return newUrl, 0
	} else if _, ok := parsedUrl.Query()["noclen"]; !ok {
		LogGeneral("Given Google Video URL is not for a fragmented stream.")
		return newUrl, 0
	} else if dataType == DtypeAudio && itag != AudioItag {
		LogGeneral("Given audio URL does not have the audio itag. Make sure you set the correct URL(s)")
		return newUrl, 0
	} else if dataType == DtypeVideo && itag == AudioItag {
		LogGeneral("Given video URL has the audio itag set. Make sure you set the correct URL(s)")
		return newUrl, 0
	}

	if sqIndex < 0 {
		sqIndex = len(gvUrl)
	}

	newUrl = gvUrl[:sqIndex] + "&sq=%d"
	return newUrl, itag
}

func RefreshURL(di *DownloadInfo, dataType, currentUrl string) {
	if !di.IsGVideoDDL() {
		newUrl := di.GetDownloadUrl(dataType)

		if len(currentUrl) == 0 || newUrl == currentUrl {
			LogDebug("%s: Attempting to retrieve a new download URL", dataType)
			di.PrintStatus()

			di.GetVideoInfo()
		}
	}
}

func ContinueFragmentDownload(di *DownloadInfo, state *fragThreadState) bool {
	if di.IsFinished(state.DataType) {
		return false
	}

	if di.FragMaxTries > 0 && state.Tries >= int(di.FragMaxTries) {
		state.FullRetries -= 1

		LogDebug("%s: Fragment %d: %d/%d retries", state.Name, state.SeqNum, state.Tries, di.FragMaxTries)
		di.PrintStatus()

		// Update video info to be safe if we are known to still be live
		if di.IsLive() {
			di.GetVideoInfo()
		}

		if !di.IsLive() || di.IsUnavailable() {
			if state.Is403 {
				if di.IsUnavailable() {
					LogWarn("%s: Download link likely expired and stream is privated or members only, cannot continue download", state.Name)
				} else {
					LogWarn("%s: Download link has likely expired and the stream has probably finished processing.", state.Name)
					LogWarn("%s: You might want to use youtube-dl to download instead.", state.Name)
				}
				di.PrintStatus()
				di.SetFinished(state.DataType)
				return false
			} else if state.MaxSeq > -1 && state.SeqNum < (state.MaxSeq-2) && state.FullRetries > 0 {
				LogDebug("%s: More than two fragments away from the highest known fragment", state.Name)
				LogDebug("%s: Will try grabbing the fragment %d more times", state.Name, state.FullRetries)
				di.PrintStatus()
			} else {
				di.SetFinished(state.DataType)
				return false
			}
		} else {
			LogDebug("%s: Fragment %d: Stream still live, continuing download attempt", state.Name, state.SeqNum)
			di.PrintStatus()
			state.Tries = 0
		}
	}

	return true
}

func HandleFragHttpError(di *DownloadInfo, state *fragThreadState, statusCode int, url string) {
	LogDebug("%s: HTTP Error for fragment %d: %d %s", state.Name, state.SeqNum, statusCode, http.StatusText(statusCode))
	di.PrintStatus()

	if statusCode == http.StatusForbidden {
		state.Is403 = true
		RefreshURL(di, state.DataType, url)
	} else if statusCode == http.StatusNotFound && state.MaxSeq > -1 && !di.IsLive() && state.SeqNum > (state.MaxSeq-2) {
		LogDebug("%s: Stream has ended and fragment within the last two not found, probably not actually created", state.Name)
		di.PrintStatus()
		di.SetFinished(state.DataType)
	}
}

func HandleFragDownloadError(di *DownloadInfo, state *fragThreadState, err error) {
	LogDebug("%s: Error with fragment %d: %s", state.Name, state.SeqNum, err)
	di.PrintStatus()

	if state.MaxSeq > -1 && !di.IsLive() && state.SeqNum >= (state.MaxSeq-2) {
		LogDebug("%s: Stream has ended and fragment number is within two of the known max, probably not actually created", state.Name)
		di.SetFinished(state.DataType)
		di.PrintStatus()
	}
}

func TryMove(srcFile, dstFile string) error {
	_, err := os.Stat(srcFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			LogWarn("Error moving file: %s", err)
			return err
		}

		return nil
	}

	LogInfo("Moving file %s to %s", srcFile, dstFile)

	err = os.Rename(srcFile, dstFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		LogWarn("Error moving file: %s", err)
		return err
	}

	return nil
}

func TryDelete(fname string) {
	_, err := os.Stat(fname)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			LogWarn("Error deleting file: %s", err)
		}

		return
	}

	LogInfo("Deleting file %s", fname)
	err = os.Remove(fname)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		LogWarn("Error deleting file: %s", err)
	}
}

// Call os.Stat and check if err is os.ErrNotExist
// Unsure if the file is guaranteed to exist when err is not nil or os.ErrNotExist
func Exists(file string) bool {
	_, err := os.Stat(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false
		}
	}

	return true
}

func CleanupFiles(files []string) {
	for _, f := range files {
		TryDelete(f)
	}
}

// Very dirty Python string formatter. Requires map keys i.e. "%(key)s"
// Throws an error if a map key is not in vals.
// This is NOT how to do a parser haha
func FormatPythonMapString(format string, vals map[string]string) (string, error) {
	pythonMapKey := regexp.MustCompile(`%\((\w+)\)s`)

	for {
		match := pythonMapKey.FindStringSubmatch(format)
		if match == nil {
			return format, nil
		}

		key := strings.ToLower(match[1])
		if _, ok := vals[key]; !ok {
			return "", fmt.Errorf("unknown output format key: '%s'", key)
		}

		val := vals[key]
		format = strings.ReplaceAll(format, match[0], val)
	}
}

func FormatFilename(format string, vals map[string]string) (string, error) {
	fnameVals := make(map[string]string)

	for k, v := range vals {
		if Contains(FilenameFormatBlacklist, k) {
			fnameVals[k] = ""
		}

		fnameVals[k] = SterilizeFilename(v)
	}

	fstr, err := FormatPythonMapString(format, fnameVals)
	if err != nil {
		return fstr, err
	}

	fnameLen := len(filepath.Base(fstr))
	if fnameLen > MaxFileNameLength {
		LogWarn("Formatted filename is too long. Truncating the title to try and fix.")
		bytesOver := fnameLen - MaxFileNameLength
		title := fnameVals["title"]
		truncateLen := len(title) - bytesOver
		fnameVals["title"] = TruncateString(title, truncateLen)
		fstr, err = FormatPythonMapString(format, fnameVals)
	}

	return fstr, err
}

// Case insensitive search. Naive linear
func Contains(arr []string, val string) bool {
	val = strings.ToLower(strings.TrimSpace(val))

	for _, s := range arr {
		if strings.ToLower(strings.TrimSpace(s)) == val {
			return true
		}
	}

	return false
}

// Logic taken from youtube-dl/yt-dlp, who got the hashing method from stack overflow
// https://github.com/yt-dlp/yt-dlp/blob/e3950399e4d471b987a2d693f8a6a476568e7c8a/yt_dlp/extractor/youtube.py#L541
// https://stackoverflow.com/a/32065323
func GenerateSAPISIDHash(origin *url.URL) string {
	var sapisidHash string
	var sapisidCookie *http.Cookie
	var papisidCookie *http.Cookie

	if origin == nil {
		return sapisidHash
	}

	cookies := client.Jar.Cookies(origin)
	if len(cookies) == 0 {
		return sapisidHash
	}

	for _, cookie := range cookies {
		if cookie.Name == "SAPISID" {
			sapisidCookie = cookie
		} else if cookie.Name == "__Secure-3PAPISID" {
			papisidCookie = cookie
		}
	}

	if sapisidCookie == nil {
		if papisidCookie == nil {
			return sapisidHash
		}

		sapisidCookie = &http.Cookie{
			Domain:   papisidCookie.Domain,
			Path:     papisidCookie.Path,
			Secure:   papisidCookie.Secure,
			Expires:  papisidCookie.Expires,
			Name:     "SAPISID",
			Value:    papisidCookie.Value,
			HttpOnly: papisidCookie.HttpOnly,
		}

		cookies = append(cookies, sapisidCookie)
		client.Jar.SetCookies(origin, cookies)
	}

	now := time.Now().Unix()
	hashBytes := sha1.Sum([]byte(fmt.Sprintf("%d %s https://www.youtube.com", now, sapisidCookie.Value)))
	sapisidHash = hex.EncodeToString(hashBytes[:])

	return fmt.Sprintf("SAPISIDHASH %d_%s", now, sapisidHash)
}

// Truncate the given string to be no more than the given number of bytes.
// Returned string may be less than maxBytes depending on the size of characters
// in the given string.
func TruncateString(s string, maxBytes int) string {
	var b strings.Builder
	r := strings.NewReader(s)
	curLen := 0
	b.Grow(r.Len())

	for {
		char, size, err := r.ReadRune()
		if err != nil {
			break
		}

		curLen += size
		if curLen > maxBytes {
			break
		}

		b.WriteRune(char)
	}

	return b.String()
}

func GetFFmpegArgs(audioFile, videoFile, thumbnail, fileDir, fileName string, onlyAudio, onlyVideo bool) FFMpegArgs {
	mergeFile := ""
	ext := ""
	ffmpegArgs := make([]string, 0, 12)
	ffmpegArgs = append(ffmpegArgs,
		"-hide_banner",
		"-nostdin",
		"-loglevel", "fatal",
		"-stats",
	)

	if downloadThumbnail && !mkv {
		ffmpegArgs = append(ffmpegArgs, "-i", thumbnail)
	}

	if onlyAudio {
		ext = "m4a"
	} else if mkv {
		ext = "mkv"
	} else {
		ext = "mp4"
	}

	mergeCounter := 0
	mergeFile = filepath.Join(fileDir, fmt.Sprintf("%s.%s", fileName, ext))
	for Exists(mergeFile) && mergeCounter < 10 {
		mergeCounter += 1
		mergeFile = filepath.Join(fileDir, fmt.Sprintf("%s-%d.%s", fileName, mergeCounter, ext))
	}

	if !onlyVideo {
		ffmpegArgs = append(ffmpegArgs,
			"-seekable", "0",
			"-thread_queue_size", "1024",
			"-i", audioFile,
		)
	}

	if !onlyAudio {
		ffmpegArgs = append(ffmpegArgs,
			"-seekable", "0",
			"-thread_queue_size", "1024",
			"-i", videoFile,
		)
		if !mkv {
			ffmpegArgs = append(ffmpegArgs, "-movflags", "faststart")
		}

		if downloadThumbnail && !mkv {
			ffmpegArgs = append(ffmpegArgs,
				"-map", "0",
				"-map", "1",
			)

			if !onlyVideo {
				ffmpegArgs = append(ffmpegArgs, "-map", "2")
			}
		}
	}

	ffmpegArgs = append(ffmpegArgs, "-c", "copy")
	if downloadThumbnail {
		if mkv {
			ffmpegArgs = append(ffmpegArgs,
				"-attach", thumbnail,
				"-metadata:s:t", "filename=cover_land.jpg",
				"-metadata:s:t", "mimetype=image/jpeg",
			)
		} else {
			ffmpegArgs = append(ffmpegArgs, "-disposition:v:0", "attached_pic")
		}
	}

	if addMeta {
		for k, v := range info.Metadata {
			if len(v) > 0 {
				ffmpegArgs = append(ffmpegArgs,
					"-metadata",
					fmt.Sprintf("%s=%s", strings.ToUpper(k), v),
				)
			}
		}
	}

	ffmpegArgs = append(ffmpegArgs, mergeFile)

	return FFMpegArgs{
		Args:     ffmpegArgs,
		FileName: mergeFile,
	}
}
