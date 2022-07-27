# ytarchive
Attempt to archive a given Youtube livestream from the start. This is most useful for streams that have already started and you want to download, but can also be used to wait for a scheduled stream and start downloading as soon as it starts. If you want to download a VOD, I recommend [yt-dlp](https://github.com/yt-dlp/yt-dlp), which is an actively maintained fork of youtube-dl with more features.

A [WebUI front-end](https://github.com/lekoOwO/ytarchive-ui) was created by leko, if that's something you want. Note that I do not use this myself and cannot comment on how well it works or looks, but it could be useful if you want to set up downloading on a remote server, or make a service out of it.

# Dependencies
- [FFmpeg](https://ffmpeg.org/) needs to be installed to mux the final file.

# Installation
## Bare Metal
Download the latest pre-release from [the releases page](https://github.com/Kethsar/ytarchive/releases)

Alternatively, if you have Go properly installed and set up, run `go install github.com/Kethsar/ytarchive@master`

`@master` is required because of some bullshit caching Go package proxies do. Should have used Rust...

## Docker

Clone the repo and run `docker build -t ytarchiver .` inside.

Or use the image from [hub.docker.com](https://hub.docker.com/r/z0pyrus/ytarchiver) (`docker pull z0pyrus/ytarchiver`)

To run the Docker Container use `docker run -it --name ytarcher /home/user/ytarchiver:/app z0pyrus/ytarchiver 'options' 'youtube url' 'quality'`. Use for `/home/user/ytarchiver` your path where the videoes will be saved.

For example `docker run -it --name ytarcher /home/user/ytarchiver:/app z0pyrus/ytarchiver '--debug' '--channel-monitor' '-r' '180' 'https://youtube.com/channel/ouhKjb89klbhjH' 'best'`

# Usage

```
usage: ytarchive [OPTIONS] [url] [quality]

	[url] is a youtube livestream URL. If not provided, you will be
	prompted to enter one.

	[quality] is a slash-delimited list of video qualities you want
	to be selected for download, from most to least wanted. If not
	provided, you will be prompted for one, with a list of available
	qualities to choose from. The following values are valid:
	audio_only, 144p, 240p, 360p, 480p, 720p, 720p60, 1080p, 1080p60, 1440p, 1440p60, 2160p, 2160p60, best

Options:
	-h
	--help
		Show this help message.

	-4
	--ipv4
		Make all connections using IPv4.

	-6
	--ipv6
		Make all connections using IPv6.

	--add-metadata
		Write some basic metadata information to the final file.

	--audio-url GOOGLEVIDEO_URL
		Pass in the given url as the audio fragment url. Must be a
		Google Video url with an itag parameter of 140.

	-c
	--cookies COOKIES_FILE
		Give a cookies.txt file that has your youtube cookies. Allows
		the script to access members-only content if you are a member
		for the given stream's user. Must be netscape cookie format.

	--debug
		Print a lot of extra information.

	-k
	--keep-ts-files
		Keep the final stream audio and video files after muxing them
		instead of deleting them.

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

	--monitor-channel
		Continually monitor a channel for streams. Requires using a /live URL.
		This will go back to checking for a stream after it finishes downloading
		the current one. Implies '-r 60 --merge' unless set separately. Minimum
		30 second wait time, 60 or more recommended. Using 'best' for quality or
		setting a decently exhaustive list recommended to prevent waiting for
		input if selected quality is not available for certain streams.
		Be careful to monitor your disk usage when using this to avoid filling
		your drive while away.

	--no-audio
		Do not download the audio stream

	--no-frag-files
		Keep fragment data in memory instead of writing to an intermediate file.
		This has the possibility to drastically increase RAM usage if a fragment
		downloads particularly slowly as more fragments after it finish first.
		This is only an issue when --threads >1
		Highly recommended if you don't have strict RAM limitations. Especially
		on Wangblows, which has caused issues with file locking when trying to
		delete fragment files.

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

	-n
	--no-wait
		Do not wait for a livestream if it's a future scheduled stream.

	-o
	--output FILENAME_FORMAT
		Set the output file name EXCLUDING THE EXTENSION. Can include
		formatting similar to youtube-dl, albeit much more limited.
		See FORMAT OPTIONS below for a list of available format keys.
		Default is '%(title)s-%(id)s'

	-r
	--retry-stream SECONDS
		If waiting for a scheduled livestream, re-check if the stream is
		up every SECONDS instead of waiting for the initial scheduled time.
		If SECONDS is less than the poll delay youtube gives (typically
		15 seconds), then this will be set to the value youtube provides.

	--save
		Automatically save any downloaded data and files if not having
		ffmpeg run when sigint is received. You will be prompted otherwise.

	--separate-audio
		Save the audio to a separate file, similar to when downloading
		audio_only, alongside the final muxed file. This includes embedding
		metadata and the thumbnail if set.

	--threads THREAD_COUNT
		Set the number of threads to use for downloading audio and video
		fragments. The total number of threads running will be
		THREAD_COUNT * 2 + 3. Main thread, a thread for each audio and
		video download, and THREAD_COUNT number of fragment downloaders
		for both audio and video.
		
		Setting this to a large number has a chance at causing the download
		to start failing with HTTP 401. Restarting the download with a smaller
		thread count until you no longer get 401s should work. Default is 1.

	-t
	--thumbnail
		Download and embed the stream thumbnail in the finished file.
		Whether the thumbnail shows properly depends on your file browser.
		Windows' seems to work. Nemo on Linux seemingly does not.

	--trace
		Print just about any information that might have reason to be printed.
		Very spammy, do not use this unless you have good reason.

	-v
	--verbose
		Print extra information.

	-V
	--version
		Print the version number and exit.

	--video-url GOOGLEVIDEO_URL
		Pass in the given url as the video fragment url. Must be a
		Google Video url with an itag parameter that is not 140.

	--vp9
		If there is a VP9 version of your selected video quality,
		download that instead of the usual h264.

	-w
	--wait
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
	ytarchive -w
		Waits for a stream. Will prompt for a URL and quality.

	ytarchive -w https://www.youtube.com/watch?v=CnWDmKx9cQQ 1080p60/best
		Waits for the given stream URL. Will prioritize downloading in 1080p60.
		If 1080p60 is not an available quality, it will choose the best of what
		is available.

	ytarchive --threads 3 https://www.youtube.com/watch?v=ZK1GXnz-1Lw best
		Downloads the given stream with 3 threads in the best available quality.
		Will ask if you want to wait if the stream is scheduled but not started.

	ytarchive -r 30 https://www.youtube.com/channel/UCZlDXzGoo7d44bwdNObFacg/live best
		Will wait for a livestream at the given URL, checking every 30 seconds.

	ytarchive -c cookies-youtube-com.txt https://www.youtube.com/watch?v=_touw1GND-M best
		Loads the given cookies file and attempts to download the given stream.
		Will ask if you want to wait.

	ytarchive --no-wait --add-metadata https://www.youtube.com/channel/UCvaTdHTWBGv3MKj3KVqJVCw/live best
		Attempts to download the given stream, and will add metadata to the
		final muxed file. Will not wait if there is no stream or if it has not
		started.

	ytarchive -o '%(channel)s/%(upload_date)s_%(title)s' https://www.youtube.com/watch?v=HxV9UAMN12o best
		Download the given stream to a directory with the channel name, and a
		file that will have the upload date and stream title. Will prompt to
		wait.

	ytarchive -w -k -t --vp9 --merge --no-frag-files https://www.youtube.com/watch?v=LE8V5iNemBA best
		Waits, keeps the final .ts files, embeds the stream thumbnail, merges
		the downloaded files if download is stopped manually, and keeps
		fragments in memory instead of writing to intermediate files.
		Downloads the stream video in VP9 if available. This set of flags will
		not require any extra user input if something goes wrong.

	ytarchive -k -t --vp9 --monitor-channel --no-frag-files https://www.youtube.com/channel/UCvaTdHTWBGv3MKj3KVqJVCw/live best
		Same as above, but waits for a stream on the given channel, and will
		repeat the cycle after downloading each stream.

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
```
