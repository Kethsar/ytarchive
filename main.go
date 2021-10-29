package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/mattn/go-colorable"
)

// Action enum
const (
	ActionAsk = iota
	ActionDo
	ActionDoNot
)

const (
	MajorVersion = 0
	MinorVersion = 3
	PatchVersion = 0
)

var (
	Commit    string
	Candidate string
)

func PrintVersion() {
	fmt.Printf("ytarchive %d.%d.%d%s%s\n", MajorVersion, MinorVersion, PatchVersion, Candidate, Commit)
}

func PrintHelp() {
	fname := filepath.Base(os.Args[0])
	qlist := MakeQualityList(VideoQualities)

	PrintVersion()
	fmt.Printf(`
usage: %[1]s [OPTIONS] [url] [quality]

	[url] is a youtube livestream URL. If not provided, you will be
	prompted to enter one.

	[quality] is a slash-delimited list of video qualities you want
	to be selected for download, from most to least wanted. If not
	provided, you will be prompted for one, with a list of available
	qualities to choose from. The following values are valid:
	%[2]s

Options:
	-h, --help
		Show this help message.

	-4, --ipv4
		Make all connections using IPv4.

	-6, --ipv6
		Make all connections using IPv6.

	--add-metadata
		Write some basic metadata information to the final file.

	--audio-url GOOGLEVIDEO_URL
		Pass in the given url as the audio fragment url. Must be a
		Google Video url with an itag parameter of 140.

	-c, --cookies COOKIES_FILE
		Give a cookies.txt file that has your youtube cookies. Allows
		the script to access members-only content if you are a member
		for the given stream's user. Must be netscape cookie format.

	--debug
		Print a lot of extra information.

	--merge
		Automatically run the ffmpeg command for the downloaded streams
		when sigint is received. You will be prompted otherwise.

	--metadata KEY=VALUE
		If writing metadata, overwrite/add metadata key-value entry.
		KEY is a metadata key that ffmpeg recognizes. If invalid, ffmpeg may ignore it or error.
		VALUE is a format template. If empty string (''), omit writing metadata for the key.
		See FORMAT TEMPLATE OPTIONS below for a list of available format keys.
		Can be used multiple times.

	--mkv
		Mux the final file into an mkv container instead of an mp4 container.
		Ignored when downloading audio only.

	--no-frag-files
		Keep fragment data in memory instead of writing to an intermediate file.
		This has the possibility to drastically increase RAM usage if a fragment
		downloads particularly slowly as more fragments after it finish first.
		This is only an issue when --threads >1

	--no-merge
		Do not run the ffmpeg command for the downloaded streams
		when sigint is received. You will be prompted otherwise.

	--no-save
		Do not save any downloaded data and files if not having ffmpeg
		run when sigint is received. You will be prompted otherwise.

	--no-video
		If a googlevideo url is given or passed with --audio-url, do not
		prompt for a video url. If a video url is given with --video-url
		then this is effectively ignored.

	-n, --no-wait
		Do not wait for a livestream if it's a future scheduled stream.

	-o, --output FILENAME_FORMAT
		Set the output file name EXCLUDING THE EXTENSION. Can include
		formatting similar to youtube-dl, albeit much more limited.
		See FORMAT OPTIONS below for a list of available format keys.
		Default is '%[3]s'

	-r, --retry-stream SECONDS
		If waiting for a scheduled livestream, re-check if the stream is
		up every SECONDS instead of waiting for the initial scheduled time.
		If SECONDS is less than the poll delay youtube gives (typically
		15 seconds), then this will be set to the value youtube provides.

	--save
		Automatically save any downloaded data and files if not having
		ffmpeg run when sigint is received. You will be prompted otherwise.

	--threads THREAD_COUNT
		Set the number of threads to use for downloading audio and video
		fragments. The total number of threads running will be
		THREAD_COUNT * 2 + 3. Main thread, a thread for each audio and
		video download, and THREAD_COUNT number of fragment downloaders
		for both audio and video.
		
		Setting this to a large number has a chance at causing the download
		to start failing with HTTP 401. Restarting the download with a smaller
		thread count until you no longer get 401s should work. Default is 1.

	-t, --thumbnail
		Download and embed the stream thumbnail in the finished file.
		Whether the thumbnail shows properly depends on your file browser.
		Windows' seems to work. Nemo on Linux seemingly does not.

	--trace
		Print just about any information that might have reason to be printed.
		Very spammy, do not use this unless you have good reason.

	-v, --verbose
		Print extra information.

	-V, --version
		Print the version number and exit.

	--video-url GOOGLEVIDEO_URL
		Pass in the given url as the video fragment url. Must be a
		Google Video url with an itag parameter that is not 140.

	--vp9
		If there is a VP9 version of your selected video quality,
		download that instead of the usual h264.

	-w, --wait
		Wait for a livestream if it's a future scheduled stream.
		If this option is not used when a scheduled stream is provided,
		you will be asked if you want to wait or not.

	--write-description
		Write the video description to a separate .description file.
	
	--write-mux-file
		Write the ffmpeg command that would mux audio and video or put audio
		into an mp4 container instead of running the command automatically.
		Useful if you want to tweak the command, want a higher log level, etc.

	--write-thumbnail
		Write the thumbnail to a separate file.

Examples:
	%[1]s -w
	%[1]s -w https://www.youtube.com/watch?v=CnWDmKx9cQQ 1080p60/best
	%[1]s --threads 3 https://www.youtube.com/watch?v=ZK1GXnz-1Lw best
	%[1]s --wait -r 30 https://www.youtube.com/channel/UCZlDXzGoo7d44bwdNObFacg/live best
	%[1]s -c cookies-youtube-com.txt https://www.youtube.com/watch?v=_touw1GND-M best
	%[1]s --no-wait --add-metadata https://www.youtube.com/channel/UCvaTdHTWBGv3MKj3KVqJVCw/live best
	%[1]s -o '%%(channel)s/%%(upload_date)s_%%(title)s' https://www.youtube.com/watch?v=HxV9UAMN12o best


FORMAT TEMPLATE OPTIONS
	Format template keys provided are made to be the same as they would be for
	youtube-dl. See https://github.com/ytdl-org/youtube-dl#output-template

	For file names, each template substitution is sanitized by replacing invalid file name
	characters with underscore (_).

	id (string): Video identifier
	url (string): Video URL
	title (string): Video title
	channel_id (string): ID of the channel
	channel (string): Full name of the channel the livestream is on
	upload_date (string: YYYYMMDD): Technically stream start date, UTC timezone - see note below
	start_date (string: YYYYMMDD): Stream start date, UTC timezone
	publish_date (string: YYYYMMDD): Stream publish date, UTC timezone
	description (string): Video description [disallowed for file name format template]

	Note on upload_date: rather than the actual upload date, stream start date is used to
	provide a better default date for youtube-dl output templates that use upload_date.
	To get the actual upload date, publish date seems to be the same as upload date for streams.
`, fname, qlist, DefaultFilenameFormat)
}

var (
	cliFlags          *flag.FlagSet
	info              *DownloadInfo
	cookieFile        string
	fnameFormat       string
	gvAudioUrl        string
	gvVideoUrl        string
	threadCount       uint
	downloadThumbnail bool
	addMeta           bool
	writeDesc         bool
	writeThumbnail    bool
	writeMuxCmd       bool
	verbose           bool
	debug             bool
	trace             bool
	noFragFiles       bool
	forceIPv4         bool
	forceIPv6         bool
	showHelp          bool
	showVersion       bool
	doWait            bool
	noWait            bool
	doMerge           bool
	noMerge           bool
	doSave            bool
	noSave            bool
	audioOnly         bool
	mkv               bool
	statusNewlines    bool
)

func init() {
	cliFlags = flag.NewFlagSet("cliFlags", flag.ExitOnError)
	info = NewDownloadInfo()

	cliFlags.BoolVar(&showHelp, "h", false, "Show the help message and exit.")
	cliFlags.BoolVar(&showHelp, "help", false, "Show the help message and exit.")
	cliFlags.BoolVar(&showVersion, "V", false, "Print the version number and exit.")
	cliFlags.BoolVar(&showVersion, "version", false, "Print the version number and exit.")
	cliFlags.BoolVar(&doWait, "w", false, "Wait for the stream to start.")
	cliFlags.BoolVar(&doWait, "wait", false, "Wait for the stream to start.")
	cliFlags.BoolVar(&noWait, "n", false, "Do not wait for the stream to start.")
	cliFlags.BoolVar(&noWait, "no-wait", false, "Do not wait for the stream to start.")
	cliFlags.BoolVar(&doMerge, "merge", false, "Auto merge files on cancelled download.")
	cliFlags.BoolVar(&noMerge, "no-merge", false, "Skip merging files on cancelled download.")
	cliFlags.BoolVar(&doSave, "save", false, "Auto save files on cancelled download.")
	cliFlags.BoolVar(&noSave, "no-save", false, "Delete all files on cancelled download.")
	cliFlags.BoolVar(&audioOnly, "no-video", false, "Only download the audio stream.")
	cliFlags.BoolVar(&noFragFiles, "no-frag-files", false, "Keep fragments in memory while waiting to write to the main file.")
	cliFlags.BoolVar(&downloadThumbnail, "t", false, "Embed thumbnail into final file.")
	cliFlags.BoolVar(&downloadThumbnail, "thumbnail", false, "Embed thumbnail into final file.")
	cliFlags.BoolVar(&verbose, "v", false, "Verbose logging output.")
	cliFlags.BoolVar(&verbose, "verbose", false, "Verbose logging output.")
	cliFlags.BoolVar(&debug, "debug", false, "Debug logging output.")
	cliFlags.BoolVar(&trace, "trace", false, "Trace logging output.")
	cliFlags.BoolVar(&info.VP9, "vp9", false, "Download VP9 video if available.")
	cliFlags.BoolVar(&addMeta, "add-metadata", false, "Write metadata to the final file.")
	cliFlags.BoolVar(&writeDesc, "write-description", false, "Write description to a separate file.")
	cliFlags.BoolVar(&writeThumbnail, "write-thumbnail", false, "Write thumbnail to a separate file.")
	cliFlags.BoolVar(&writeMuxCmd, "write-mux-file", false, "Write the command that will be used for muxing to a file. Does not merge the final file.")
	cliFlags.BoolVar(&forceIPv4, "4", false, "Force IPv4 connections.")
	cliFlags.BoolVar(&forceIPv4, "ipv4", false, "Force IPv4 connections.")
	cliFlags.BoolVar(&forceIPv6, "6", false, "Force IPv6 connections.")
	cliFlags.BoolVar(&forceIPv6, "ipv6", false, "Force IPv6 connections.")
	cliFlags.BoolVar(&mkv, "mkv", false, "Make the final container mkv (ignored when audio only).")
	cliFlags.BoolVar(&statusNewlines, "newline", false, "Write progress to a new line instead of keeping it on one line.")
	cliFlags.StringVar(&cookieFile, "c", "", "Cookies to be used when downloading.")
	cliFlags.StringVar(&cookieFile, "cookies", "", "Cookies to be used when downloading.")
	cliFlags.StringVar(&fnameFormat, "o", DefaultFilenameFormat, "Filename output format.")
	cliFlags.StringVar(&fnameFormat, "output", DefaultFilenameFormat, "Filename output format.")
	cliFlags.IntVar(&info.RetrySecs, "r", 0, "Seconds to wait between checking stream status.")
	cliFlags.IntVar(&info.RetrySecs, "retry-stream", 0, "Seconds to wait between checking stream status.")
	cliFlags.UintVar(&threadCount, "threads", 1, "Number of download threads for each stream type.")

	cliFlags.Func("video-url", "Googlevideo URL for the video stream.", func(s string) error {
		var itag int
		gvVideoUrl, itag = ParseGvideoUrl(s, DtypeVideo)

		if itag == 0 {
			return errors.New("invalid video URL given with --video-url")
		}

		return nil
	})

	cliFlags.Func("audio-url", "Googlevideo URL for the audio stream.", func(s string) error {
		var itag int
		gvAudioUrl, itag = ParseGvideoUrl(s, DtypeAudio)

		if itag == 0 {
			return errors.New("invalid audio URL given with --audio-url")
		}

		return nil
	})

	cliFlags.Func("metadata", "Metadata fields to add in KEY=VALUE format.", func(s string) error {
		parts := strings.Split(s, "=")
		if len(parts) > 2 {
			return nil
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		info.Metadata[key] = val

		return nil
	})
}

// ehh, bad way to do this probably but allows deferred functions to run
// while also allowing early return with a non-0 exit code.
func run() int {
	cliFlags.Parse(os.Args[1:])
	colorable.EnableColorsStdout(nil)

	if showHelp {
		PrintHelp()
		return 1
	}

	if showVersion {
		PrintVersion()
		return 0
	}

	mergeOnCancel := ActionAsk
	saveOnCancel := ActionAsk

	if trace {
		loglevel = LoglevelTrace
		verbose = true
	} else if debug {
		loglevel = LoglevelDebug
		verbose = true
	} else if verbose {
		loglevel = LogleveInfo
	}
	log.SetPrefix("\r")

	if doWait {
		info.Wait = ActionDo
	} else if noWait {
		info.Wait = ActionDoNot
	}

	if doMerge {
		mergeOnCancel = ActionDo
	} else if noMerge {
		mergeOnCancel = ActionDoNot
	}

	if doSave {
		saveOnCancel = ActionDo
	} else if noSave {
		saveOnCancel = ActionDoNot
	}

	if audioOnly {
		info.Quality = AudioOnlyQuality
	}

	if forceIPv4 {
		networkType = NetworkIPv4
	} else if forceIPv6 {
		networkType = NetworkIPv6
	}

	if noFragFiles {
		info.FragFiles = false
	}

	if info.RetrySecs > 0 && info.RetrySecs < DefaultPollTime {
		info.RetrySecs = DefaultPollTime
	}

	if threadCount > 1 {
		info.Jobs = int(threadCount)
	}

	if len(gvVideoUrl) > 0 {
		info.URL = gvVideoUrl
		info.SetDownloadUrl(DtypeVideo, gvVideoUrl)
	}

	if len(gvAudioUrl) > 0 {
		if len(info.URL) == 0 {
			info.URL = gvAudioUrl
		}

		info.SetDownloadUrl(DtypeAudio, gvAudioUrl)
	}

	if len(info.URL) == 0 {
		if cliFlags.NArg() > 1 {
			info.URL = cliFlags.Arg(0)
			info.SelectedQuality = cliFlags.Arg(1)
		} else if cliFlags.NArg() == 1 {
			info.URL = cliFlags.Arg(0)
		} else {
			info.URL = GetUserInput("Enter a youtube livestream URL: ")
		}
	}

	err := info.ParseInputUrl()
	if err != nil {
		LogError(err.Error())
		return 1
	}

	_, err = FormatFilename(fnameFormat, info.FormatInfo)
	if err != nil {
		LogError(err.Error())
		return 1
	}

	if len(cookieFile) > 0 {
		cjar, err := info.ParseNetscapeCookiesFile(cookieFile)
		if err != nil {
			LogError("Failed to load cookies file: %s", err)
			return 1
		}

		client.Jar = cjar
		LogInfo("Loaded cookie file %s", cookieFile)
	}

	if !info.GVideoDDL && !info.GetVideoInfo() {
		return 1
	}

	// We checked if there would be errors earlier, should be good
	fullFPath, _ := FormatFilename(fnameFormat, info.FormatInfo)
	fdir := filepath.Dir(fullFPath)
	tmpDir := ""

	if !strings.HasPrefix(fnameFormat, string(os.PathSeparator)) {
		fdir = strings.TrimLeft(fdir, string(os.PathSeparator))
	}
	if len(strings.TrimSpace(fdir)) == 0 {
		fdir = "."
	}

	fname := filepath.Base(fullFPath)
	fname = SterilizeFilename(fname)

	if strings.HasPrefix(fname, "-") {
		fname = "_" + fname
	}

	if fname == "." || len(strings.TrimSpace(fname)) == 0 {
		LogError("Output file name appears to be empty after formatting.")
		LogError("Expanded output file path: %s", fullFPath)
		return 1
	}

	if fdir != "." {
		err = os.MkdirAll(fdir, 0755)
		if err != nil {
			LogWarn("Error creating final file directory: %s", err)
			LogWarn("The final file will be placed in the current working directory")
			fdir = "."
		}
	}

	tmpDir, err = os.MkdirTemp(fdir, fmt.Sprintf("%s__", info.VideoID))
	if err != nil {
		LogWarn("Error creating temp directory: %s", err)
		LogWarn("Will download data directly to %s instead", fdir)
		tmpDir = fdir
	}

	afileName := fmt.Sprintf("%s.f%d", fname, AudioItag)
	vfileName := fmt.Sprintf("%s.f%d", fname, info.Quality)
	thmbnlName := fmt.Sprintf("%s.jpg", fname)
	descFileName := fmt.Sprintf("%s.description", fname)
	muxFileName := fmt.Sprintf("%s.ffmpeg_command.txt", fname)

	info.MDLInfo[DtypeAudio].BasePath = filepath.Join(tmpDir, afileName)
	info.MDLInfo[DtypeVideo].BasePath = filepath.Join(tmpDir, vfileName)

	afile := info.MDLInfo[DtypeAudio].BasePath + ".ts"
	vfile := info.MDLInfo[DtypeVideo].BasePath + ".ts"
	thmbnlFile := filepath.Join(tmpDir, thmbnlName)
	descFile := filepath.Join(tmpDir, descFileName)

	progressChan := make(chan *ProgressInfo, info.Jobs*2)
	var totalBytes int64
	frags := map[string]int{
		DtypeAudio: 0,
		DtypeVideo: 0,
	}

	if (downloadThumbnail || writeThumbnail) && len(info.Thumbnail) > 0 {
		downloaded := DownloadThumbnail(info.Thumbnail, thmbnlFile)

		if !downloaded {
			TryDelete(thmbnlFile)
			downloadThumbnail = false
			writeThumbnail = false
		}
	} else {
		downloadThumbnail = false
		writeThumbnail = false
	}

	if writeDesc && len(info.Metadata["comment"]) > 0 {
		err = os.WriteFile(descFile, []byte(info.Metadata["comment"]), 0644)

		if err != nil {
			LogWarn("Error writing description file: %s", err)
			TryDelete(descFile)
		}
	}

	dlDoneChan := make(chan struct{}, 2)
	activeDownloads := 0

	LogInfo("Starting download to %s", afile)
	go info.DownloadStream(DtypeAudio, afile, progressChan, dlDoneChan)
	activeDownloads += 1

	if len(info.GetDownloadUrl(DtypeVideo)) > 0 {
		LogInfo("Starting download to %s", vfile)
		go info.DownloadStream(DtypeVideo, vfile, progressChan, dlDoneChan)
		activeDownloads += 1
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	maxSeq := -1
	for {

		select {
		case progress := <-progressChan:
			totalBytes += int64(progress.ByteCount)
			frags[progress.DataType] += 1

			if progress.MaxSeq > maxSeq {
				maxSeq = progress.MaxSeq
			}

			status := "\r"
			if statusNewlines {
				status = ""
			}

			status += fmt.Sprintf("Video Fragments: %d; Audio Fragments: %d; ", frags[DtypeVideo], frags[DtypeAudio])
			if verbose {
				status += fmt.Sprintf("Max Sequence: %d; ", maxSeq)
			}

			status += fmt.Sprintf("Total Downloaded: %s", FormatSize(totalBytes))
			if statusNewlines {
				status += "\n"
			} else {
				status += "\033[K"
			}

			info.SetStatus(status)
		case <-sigChan:
			signal.Reset(os.Interrupt)
			info.Stop()
			fmt.Println()
			LogWarn("User Interrupt, Stopping download...")

			for activeDownloads > 0 {
				select {
				case <-progressChan:
				case <-dlDoneChan:
					activeDownloads -= 1
				}
			}

			fmt.Println()
			merge := false
			if mergeOnCancel == ActionAsk {
				merge = GetYesNo("\nDownload stopped prematurely. Would you like to merge the currently downloaded data?")
			} else if mergeOnCancel == ActionDo {
				merge = true
			}

			if !merge {
				saveFiles := false
				if saveOnCancel == ActionAsk {
					saveFiles = GetYesNo("\nWould you like to save any created files?")
				} else if saveOnCancel == ActionDo {
					saveFiles = true
				}

				if saveFiles {
					TryMove(afile, filepath.Join(fdir, fmt.Sprintf("%s.ts", afileName)))
					TryMove(vfile, filepath.Join(fdir, fmt.Sprintf("%s.ts", vfileName)))
					TryMove(thmbnlFile, filepath.Join(fdir, thmbnlName))
					TryMove(descFile, filepath.Join(fdir, descFileName))
				}

				return 2
			}
		case <-dlDoneChan:
			activeDownloads -= 1
		}

		if activeDownloads <= 0 {
			break
		}
	}

	signal.Reset(os.Interrupt)
	fmt.Println("\nDownload Finished")

	audioOnly = info.Quality == AudioOnlyQuality
	if !audioOnly && frags[DtypeAudio] != frags[DtypeVideo] {
		LogWarn("Mismatched number of video and audio fragments.")
		LogWarn("The files should still be mergable but data might be missing.")
	}

	newAudioFile := filepath.Join(fdir, fmt.Sprintf("%s.ts", afileName))
	newVideoFile := filepath.Join(fdir, fmt.Sprintf("%s.ts", vfileName))
	newThumbnail := filepath.Join(fdir, thmbnlName)
	newDescFile := filepath.Join(fdir, descFileName)
	muxFile := filepath.Join(fdir, muxFileName)

	TryMove(afile, newAudioFile)
	TryMove(vfile, newVideoFile)
	TryMove(thmbnlFile, newThumbnail)
	TryMove(descFile, newDescFile)

	filesToDel := make([]string, 0, 3)
	filesToDel = append(filesToDel, newAudioFile, newVideoFile)
	if !writeThumbnail {
		filesToDel = append(filesToDel, newThumbnail)
	}

	// TODO: Remove the defer function above to delete the tmpdir and check that
	// all TryMove() calls completed instead, so as to not delete something we
	// don't mean to

	retcode := 0
	mergeFile := ""
	ext := ""
	ffmpegArgs := make([]string, 0, 12)
	ffmpegArgs = append(ffmpegArgs,
		"-hide_banner",
		"-nostdin",
		"-loglevel", "fatal",
		"-stats",
		"-i", newAudioFile,
	)

	if downloadThumbnail && !mkv {
		ffmpegArgs = append(ffmpegArgs, "-i", newThumbnail)
	}

	if mkv {
		ext = "mkv"
	} else if audioOnly {
		ext = "m4a"
	} else {
		ext = "mp4"
	}

	mergeFile = filepath.Join(fdir, fmt.Sprintf("%s.%s", fname, ext))

	if !audioOnly {
		ffmpegArgs = append(ffmpegArgs, "-i", newVideoFile)
		if !mkv {
			ffmpegArgs = append(ffmpegArgs, "-movflags", "faststart")
		}

		if downloadThumbnail && !mkv {
			ffmpegArgs = append(ffmpegArgs,
				"-map", "0",
				"-map", "1",
				"-map", "2",
			)
		}
	}

	ffmpegArgs = append(ffmpegArgs, "-c", "copy")
	if downloadThumbnail {
		if mkv {
			ffmpegArgs = append(ffmpegArgs,
				"-attach", newThumbnail,
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

	mergeCounter := 0
	for Exists(mergeFile) && mergeCounter < 10 {
		mergeCounter += 1
		mergeFile = filepath.Join(fdir, fmt.Sprintf("%s-%d.%s", fname, mergeCounter, ext))
	}

	ffmpegArgs = append(ffmpegArgs, mergeFile)
	ffmpegCmd := "ffmpeg " + shellescape.QuoteCommand(ffmpegArgs)

	if writeMuxCmd {
		fmt.Printf("Writing ffmpeg command to create the final file to %s\n", muxFile)
		err = os.WriteFile(muxFile, []byte(ffmpegCmd), 0644)
		if err != nil {
			LogError("Error writing muxcmd file: %s", err.Error())
			return 1
		}

		return 0
	}

	_, err = exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Println(ffmpegCmd)
		LogError("\nffmpeg not found. Please install ffmpeg")

		fmt.Printf("Writing ffmpeg command to create the final file to %s\n", muxFile)
		err = os.WriteFile(muxFile, []byte(ffmpegCmd), 0644)
		if err != nil {
			fmt.Println(ffmpegCmd)
			LogError("\nError writing muxcmd file: %s", err.Error())
			LogError("The command has been ouput above instead.")
		}

		return 1
	}

	retcode = Execute("ffmpeg", ffmpegArgs)
	if retcode != 0 {
		LogError("Execute returned code %d. Something must have gone wrong with ffmpeg.", retcode)
		LogError("The .ts files will not be deleted in case the final file is broken.")
		return retcode
	}

	CleanupFiles(filesToDel)
	if tmpDir != fdir {
		os.RemoveAll(tmpDir)
	}

	fmt.Printf("%[1]sFinal file: %[2]s%[1]s", "\n", mergeFile)

	return 0
}

func main() {
	os.Exit(run())
}
