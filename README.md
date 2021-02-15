# ytarchive
Attempt to archive a given Youtube livestream from the start. This is most useful for streams that have already started and you want to download. If you know ahead of time you want to archive a stream, it would probably be better to use [streamlink](https://streamlink.github.io/) or [youtube-dl](https://youtube-dl.org/), as both are more robust and more likely to be continually updated. Probably. The only dowside being neither can grab VP9 for streams where available.

# Dependencies
- [FFmpeg](https://ffmpeg.org/) needs to be installed to mux the final file.

- For *nix systems, Python 3 is required, and should already be installed unless your system is 10 years old.

- For Windows, an all-in-one binary is provided for each release. If you want to run the script on its own, you will also need Python 3.

# Usage

```
usage: ytarchive.py [OPTIONS] [url] [quality]

	[url] is a youtube livestream URL. If not provided, you will be
	prompted to enter one.

	[quality] is a slash-delimited list of video qualities you want
	to be selected for download, from most to least wanted. If not
	provided, you will be prompted for one, with a list of available
	qualities to choose from. The following values are valid:
	audio_only, 144p, 240p, 360p, 480p, 720p, 720p60, 1080p, 1080p60, best

Options:
	-h, --help
		Show this help message.

	--add-metadata
		Write some basic metadata information to the final file.

	-c, --cookies COOKIES_FILE
		Give a cookies.txt file that has your youtube cookies. Allows
		the script to access members-only content if you are a member
		for the given stream's user. Must be netscape cookie format.

	--debug
		Print a lot of extra information.

	-n, --no-wait
		Do not wait for a livestream if it's a future scheduled stream.

	-o, --output FILENAME_FORMAT
		Set the output file name EXCLUDING THE EXTENSION. Can include
		formatting similar to youtube-dl, albeit much more limited.
		See FORMAT OPTIONS below for a list of available format keys.
		Default is '%(title)s-%(id)s'

	-r, --retry-stream SECONDS
		If waiting for a scheduled livestream, re-check if the stream is
		up every SECONDS instead of waiting for the initial scheduled time.

	--threads THREAD_COUNT
		Set the number of threads to use for downloading audio and video
		fragments. The total number of threads running will be
		THREAD_COUNT * 2 + 3. Main thread, a thread for each audio and
		video download, and THREAD_COUNT number of fragment downloaders
		for both audio and video. The nature of Python means this script
		will never use more than a single CPU core no matter how many
		threads are started. Setting this above 5 is not recommended.
		Default is 1.

	-t, --thumbnail
		Download and embed the stream thumbnail in the finished file.
		Whether the thumbnail shows properly depends on your file browser.
		Windows' seems to work. Nemo on Linux seemingly does not.

	-v, --verbose
		Print extra information.

	--vp9
		If there is a VP9 version of your selected video quality,
		download that instead of the usual h264.

	-w, --wait
		Wait for a livestream if it's a future scheduled stream.
		If this option is not used when a scheduled stream is provided,
		you will be asked if you want to wait or not.

Examples:
	ytarchive.py -w
	ytarchive.py -w https://www.youtube.com/watch?v=CnWDmKx9cQQ 1080p60/best
	ytarchive.py --threads 3 https://www.youtube.com/watch?v=ZK1GXnz-1Lw best
	ytarchive.py --wait -r 30 https://www.youtube.com/channel/UCZlDXzGoo7d44bwdNObFacg/live best
	ytarchive.py -c cookies-youtube-com.txt https://www.youtube.com/watch?v=_touw1GND-M best
	ytarchive.py --no-wait --add-metadata https://www.youtube.com/channel/UCvaTdHTWBGv3MKj3KVqJVCw/live best
	ytarchive.py -o '%(channel)s/%(upload_date)s_%(title)s' https://www.youtube.com/watch?v=HxV9UAMN12o best


FORMAT OPTIONS
	Format keys provided are made to be the same as they would be for
	youtube-dl. See https://github.com/ytdl-org/youtube-dl#output-template

	id (string): Video identifier
	title (string): Video title
	channel_id (string): ID of the channel
	channel (string): Full name of the channel the livestream is on
	upload_date (string): Technically stream date (YYYYMMDD)
```