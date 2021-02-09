#!/bin/env python3
import urllib.parse
import urllib.request
import urllib.error
import http.client
import http.cookiejar
import json
import sys
import time
import calendar
import threading
import os
import queue
import shutil
import subprocess
import getopt
import logging

# Constants
INFO_URL = "https://www.youtube.com/get_video_info?video_id={0}&el=detailpage"
HTML_VIDEO_LINK_TAG = '<link rel="canonical" href="https://www.youtube.com/watch?v='
PLAYABLE_OK = "OK"
PLAYABLE_OFFLINE = "LIVE_STREAM_OFFLINE"
PLAYABLE_UNPLAYABLE = "UNPLAYABLE"
PLAYABLE_ERROR = "ERROR"
BAD_CHARS = '<>:"/\\|?*'
DTYPE_AUDIO = "audio"
DTYPE_VIDEO = "video"
DEFAULT_VIDEO_QUALITY = "best"
RECHECK_TIME = 15
WAIT_ASK = 0
WAIT = 1
NO_WAIT = 2
FRAG_MAX_TRIES = 10
FRAG_MAX_EMPTY = 10

# https://gist.github.com/AgentOak/34d47c65b1d28829bb17c24c04a0096f
AUDIO_ITAG = 140
VIDEO_LABEL_ITAGS = {
	'audio_only': 0, 
	'144p': {"h264": 160, "vp9": 278},
	'240p': {"h264": 133, "vp9": 242},
	'360p': {"h264": 134, "vp9": 243},
	'480p': {"h264": 135, "vp9": 244},
	'720p': {"h264": 136, "vp9": 247},
	'720p60': {"h264": 298, "vp9": 302},
	'1080p': {"h264": 137, "vp9": 248},
	'1080p60': {"h264": 299, "vp9": 303},
}

# Simple class to more easily keep track of what fields are available for
# file name formatting
class FormatInfo:
	finfo = {
		"id": "",
		"title": "",
		"channel_id": "",
		"channel": "",
		"upload_date": ""
	}

	def get_info(self):
		return self.finfo

	def set_info(self, player_response):
		pmfr = player_response["microformat"]["playerMicroformatRenderer"]
		vid_details = player_response["videoDetails"]

		self.finfo["id"] = sterilize_filename(vid_details["videoId"])
		self.finfo["title"] = sterilize_filename(vid_details["title"])
		self.finfo["channel_id"] = sterilize_filename(vid_details["channelId"])
		self.finfo["channel"] = sterilize_filename(vid_details["author"])
		self.finfo["upload_date"] = sterilize_filename(pmfr["uploadDate"].replace("-", ""))

# Info to be sent through the progress queue
class ProgressInfo:
	def __init__(self, dtype, byte_count, max_seq):
		self.data_type = dtype
		self.bytes = byte_count
		self.max_seq = max_seq

# Fragment data
class Fragment:
	def __init__(self, seq, data, header_seqnum):
		self.seq = seq
		self.data = data
		self.x_head_seqnum = header_seqnum

# Metadata for the final file
class MetaInfo:
	meta = {
		"title": "",
		"artist": "",
		"date": "",
		"description": ""
	}

	def get_meta(self):
		return self.meta

	def set_meta(self, player_response):
		pmfr = player_response["microformat"]["playerMicroformatRenderer"]
		vid_details = player_response["videoDetails"]
		url = "https://www.youtube.com/watch?v={0}".format(vid_details["videoId"])

		self.meta["title"] = vid_details["title"]
		self.meta["artist"] = vid_details["author"]
		self.meta["date"] = pmfr["uploadDate"].replace("-", "")
		# MP4 doesn't allow for a url metadata field
		# Just put it at the top of the description
		self.meta["description"] = "{0}\n\n{1}".format(url, vid_details["shortDescription"])

# Miscellaneous information
class DownloadInfo:
	def __init__(self):
		# Python may have the GIL but it's better to be safe
		# RLock so we can lock multiple times in the same thread without deadlocking
		self.lock = threading.RLock()
		self.format_info = FormatInfo()
		self.metadata = MetaInfo()
		self.stopping = False
		self.in_progress = False
		self.is_live = False
		self.vp9 = False
		self.thumbnail = ""
		self.vid = ""
		self.url = ""
		self.selected_quality = ""
		self.wait = WAIT_ASK
		self.quality = -1
		self.retry_secs = 0
		self.thread_count = 1 # Make into dict 
		self.last_updated = 0
		self.target_duration = 5
		self.download_urls = {
			DTYPE_VIDEO: "",
			DTYPE_AUDIO: ""
		}
		self.active_threads = {
			DTYPE_VIDEO: 0,
			DTYPE_AUDIO: 0
		}

# Remove any illegal filename chars
# Not robust, but the combination of video title and id should prevent other illegal combinations
def sterilize_filename(fname):
	for c in BAD_CHARS:
		fname = fname.replace(c, "_")

	return fname

# Pretty formatting of byte count
def format_size(bsize):
	postfixes = ["bytes", "KiB", "MiB", "GiB"] # don't even bother with terabytes
	i = 0
	while bsize > 1024:
		bsize = bsize/1024
		i += 1
	
	return "{0:.2f}{1}".format(bsize, postfixes[i])

# Execute an external process using the given args
# Returns the process return code, or -1 on unknown error
def execute(args):
	retcode = 0
	logging.debug("Executing command: {0}".format(" ".join(args)))

	try:
		if sys.version_info.major == 3 and sys.version_info.minor >= 5:
			retcode = subprocess.run(args).returncode
		else:
			retcode = subprocess.call(args)
	except Exception as err:
		logging.error(err)
		retcode = -1

	return retcode

# Download data from the given URL and return it as unicode text
def download_as_text(url):
	data = b''

	try:
		with urllib.request.urlopen(url, timeout=5) as resp:
			data = resp.read()
	except Exception as err:
		logging.warning("Failed to retrieve data from {0}: {1}".format(url, err))
		return None
	
	return data.decode("utf-8")

def download_thumbnail(url, fname):
	try:
		with urllib.request.urlopen(url, timeout=5) as resp:
			with open(fname, "wb") as f:
				f.write(resp.read())
	except Exception as err:
		logging.warning("Failed to download thumbnail: {0}".format(err))
		return False

	return True

# Get the base player response object for the given video id
def get_player_response(vid):
	vinfo = download_as_text(INFO_URL.format(vid))

	if len(vinfo) == 0:
		logging.warning("No video information found, somehow")
		return None

	parsedinfo = urllib.parse.parse_qs(vinfo)
	player_response = json.loads(parsedinfo['player_response'][0])
	return player_response

# Make a comma-separated list of available formats
def make_quality_list(formats):
	qualities = ""
	quarity = ""

	for f in formats:
		qualities += f + ", "

	qualities += "best"
	return qualities

# Parse the user-given list of qualities they are willing to accept for download
def parse_quality_list(formats, quality):
	selected_qualities = []
	quality = quality.lower().strip()
	if "best" not in formats:
		formats.append("best")

	selected_quarities = quality.split('/')
	for q in selected_quarities:
		if q.strip() in formats:
			selected_qualities.append(q)
		
	if len(selected_qualities) < 1:
		print("No valid qualities selected")

	return selected_qualities

# Prompt the user to select a video quality
def get_quality_from_user(formats, waiting=False):
	if waiting:
		print("Since you are going to wait for the stream, you must pre-emptively select a video quality.")
		print("There is no way to know which qualities will be available before the stream starts, so a list of all possible stream qualities will be presented.")
		print("You can use youtube-dl style selection (slash-delimited first to last preference). Default is 'best'\n")
	
	quarity = ""
	selected_qualities = []
	qualities = make_quality_list(formats)
	print("Available video qualities: {0}".format(qualities))

	while len(selected_qualities) < 1:
		quarity = input("Enter desired video quality: ")
		quarity = quarity.lower().strip()
		if quarity == "":
			quarity = DEFAULT_VIDEO_QUALITY

		selected_qualities = parse_quality_list(formats, quarity)

	return selected_qualities

def get_yes_no(msg):
	yesno = input(msg)
	return yesno.lower() == "y"

# Ask if the user wants to wait for a scheduled stream to start and then record it
def ask_wait_for_stream(url):
	print("{0} is probably a future scheduled livestream.".format(url))
	print("I would highly recommend using streamlink with the --retry-streams argument.")
	print("Example: streamlink --retry-streams=15 -o 'title.mp4' '{0}' best".format(url))
	print()
	print("I can do this instead of you don't have streamlink.")

	return get_yes_no("Wait for the livestream and record it? [y/N]: ")

# Find the logged_in value from the given player_response object
# Useful for checking if a cookies file is working properly
def get_logged_in(player_response):
	serv_track_params = player_response["responseContext"]["serviceTrackingParams"]

	for stp in serv_track_params:
		params = stp["params"]

		for p in params:
			if p["key"] == "logged_in":
				return p["value"]

# Keep retrieving the player response object until the playability status is OK
def get_playable_player_response(info):
	first_wait = True
	retry = True
	player_response = {}
	secs_late = 0
	selected_qualities = []
	vid = info.vid
	url = info.url

	while retry:
		player_response = get_player_response(vid)
		if not player_response:
			return None


		if not 'videoDetails' in player_response:
			print("Video Details not found, video is likely private or does not exist.")
			if info.in_progress:
				info.is_live = False
			return None

		if not player_response['videoDetails']['isLiveContent']:
			print("{0} is not a livestream. It would be better to use youtube-dl to download it.".format(url))
			return None

		playability = player_response['playabilityStatus']
		playability_status = playability['status']

		if info.selected_quality:
			selected_qualities = parse_quality_list(list(VIDEO_LABEL_ITAGS.keys()), info.selected_quality)

		if playability_status == PLAYABLE_ERROR:
			print("Playability status: ERROR. Reason: {0}".format(playability["reason"]))
			if info.in_progress:
				print("Finishing download")
				info.is_live = False
			return None

		elif playability_status == PLAYABLE_UNPLAYABLE:
			print("Playability status: Unplayable.")
			print("Reason: {0}".format(playability["reason"]))
			print("Logged in status: {0}".format(get_logged_in(player_response)))
			print("If this is a members only stream, you provided a cookies.txt file, and the above 'logged in' status is not '1', please try updating your cookies file.")
			print("Also check if your cookies file includes '#HttpOnly_' in front of some lines. If it does, delete that part of those lines and try again.")
			return None

		elif playability_status == PLAYABLE_OFFLINE:
			# We've already started downloading, stream might be experiencing issues
			if info.in_progress:
				logging.debug("Livestream status is {0} mid-download".format(PLAYABLE_OFFLINE))
				return None

			if info.wait == NO_WAIT:
				print("Stream appears to be a future scheduled stream, and you opted not to wait.")
				return None

			if first_wait and info.wait == WAIT_ASK:
				if not ask_wait_for_stream(url):
					return None

			if first_wait:
				print()
				if len(selected_qualities) < 1:
					selected_qualities = get_quality_from_user(list(VIDEO_LABEL_ITAGS.keys()), True)

			if info.retry_secs > 0:
				if first_wait:
					print("Waiting for stream, retrying every {0} seconds...".format(info.retry_secs))

				first_wait = False
				time.sleep(info.retry_secs)
				continue

			# Jesus fuck youtube, embed some more objects why don't you
			sched_time = int(playability["liveStreamability"]["liveStreamabilityRenderer"]["offlineSlate"]["liveStreamOfflineSlateRenderer"]["scheduledStartTime"])
			cur_time = int(time.time())
			slep_time = sched_time - cur_time

			if slep_time > 0:
				if not first_wait:
					if secs_late > 0:
						print()
					print("Stream rescheduled")

				first_wait = False
				secs_late = 0

				print("Stream starts in {0} seconds. Waiting for this time to elapse...".format(slep_time))

				# Loop it just in case a rogue sleep interrupt happens
				while slep_time > 0:
					# There must be a better way but whatever
					time.sleep(slep_time)
					cur_time = int(time.time())
					slep_time = sched_time - cur_time

					if slep_time > 0:
						logging.debug("Woke up {0} seconds early. Continuing sleep...".format(slep_time))

				# We've waited until the scheduled time
				continue

			first_wait = False

			# If we get this far, the stream's scheduled time has passed but it's still not started
			# Check every 15 seconds
			if not first_wait:
				time.sleep(RECHECK_TIME)
				secs_late += RECHECK_TIME
				print("\rStream is {0} seconds late...".format(secs_late), end="")
				continue

		elif playability_status != PLAYABLE_OK:
			if secs_late > 0:
				print()
				
			logging.warning("Unknown playability status: {0}".format(playability_status))
			if info.in_progress:
				info.is_live = False
			return None

		if secs_late > 0:
			print()
		retry = False

	return {"player_response": player_response, "selected_qualities": selected_qualities}

def is_fragmented(url):
	# Per anon, there will be a noclen query parameter if the given URLs
	# are meant to be downloaded in fragments. Else it will have a clen
	# parameter obviously specifying content length.
	queries = urllib.parse.parse_qs(urllib.parse.urlparse(url).query)
	return "noclen" in queries

# Get necessary video info such as video/audio URLs
# Stores them in info
def get_video_info(info):
	with info.lock: # Because I forgot some releases, this is worth the extra indent
		if info.stopping:
			return False

		# Almost nothing we care about is likely to change in 15 seconds,
		# except maybe whether the livestream is online
		update_delta = time.time() - info.last_updated
		if update_delta < RECHECK_TIME:
			return False

		vals = get_playable_player_response(info)
		if not vals:
			return False

		player_response = vals["player_response"]
		selected_qualities = vals["selected_qualities"]
		video_details = player_response["videoDetails"]
		pmfr = player_response["microformat"]["playerMicroformatRenderer"]
		live_details = pmfr["liveBroadcastDetails"]
		is_live = live_details["isLiveNow"]

		if not is_live and not info.in_progress:
			# Likely the livestream ended already.
			# Check if the stream has been mostly processed.
			# If not then download it. Else youtube-dl is a better choice.
			if "endTimestamp" in live_details:
				# Assume that all formats will be fully processed if one is, and vice versa
				if not "url" in player_response["streamingData"]["adaptiveFormats"][0]:
					print("Livestream has ended and is being processed. Download URLs are not available.")
					return False

				url = player_response["streamingData"]["adaptiveFormats"][0]["url"]
				if not is_fragmented(url):
					print("Livestream has been processed, use youtube-dl instead.")
					return False
			else:
				print("Livestream is offline, should have started, but has no end timestamp.")
				print("You could try again, or try youtube-dl.")
				return False
		
		formats = player_response["streamingData"]["adaptiveFormats"]

		if info.quality < 0:
			qualities = ["audio_only"]
			itags = list(VIDEO_LABEL_ITAGS.keys())
			found = False

			# Generate a list of available qualities, sorted in order from best to worst
			# Assuming if VP9 is available, h264 should be available for that quality too
			for fmt in formats:
				if fmt["mimeType"].startswith("video/mp4"):
					qlabel = fmt["qualityLabel"].lower()
					priority = itags.index(qlabel)
					idx = 0

					for q in qualities:
						p = itags.index(q)
						if p > priority:
							break
						
						idx += 1

					qualities.insert(idx, qlabel)

			while not found:
				if len(selected_qualities) == 0:
					selected_qualities = get_quality_from_user(qualities)

				for q in selected_qualities:
					q = q.strip()

					# Find the best quality of those availble.
					# This is why we sorted the list as we made it.
					if q == "best":
						itags = list(VIDEO_LABEL_ITAGS.keys())
						best = 0

						for fmt in formats:
							if 'qualityLabel' not in fmt:
								continue

							qlabel = fmt["qualityLabel"].lower()
							priority = itags.index(qlabel)

							if priority > best:
								best = priority

						q = itags[best]

					video_itag = VIDEO_LABEL_ITAGS[q]
					aonly = video_itag == VIDEO_LABEL_ITAGS["audio_only"]

					if aonly:
						info.quality = video_itag
						info.download_urls[DTYPE_VIDEO] = ""
						found = True

					for fmt in formats:
						if not aonly:
							if fmt["itag"] == video_itag["h264"] and not found:
								info.quality = fmt["itag"]
								info.download_urls[DTYPE_VIDEO] = fmt["url"] + "&sq={0}"
								found = True
								print("Selected quality: {0} (h264)".format(q))
							elif info.vp9 and fmt["itag"] == video_itag["vp9"]:
								info.quality = fmt["itag"]
								info.download_urls[DTYPE_VIDEO] = fmt["url"] + "&sq={0}"

								if not found:
									print("Selected quality: {0} (VP9)".format(q))
									found = True
								else:
									print("VP9 of the same quality found, using that instead")
							elif fmt["itag"] == AUDIO_ITAG:
								info.download_urls[DTYPE_AUDIO] = fmt["url"] + "&sq={0}"
								info.target_duration = fmt["targetDurationSec"]
						elif fmt["itag"] == AUDIO_ITAG:
							info.download_urls[DTYPE_AUDIO] = fmt["url"] + "&sq={0}"
							info.target_duration = fmt["targetDurationSec"]

					if found:
						break
			
			# None of the qualities the user gave were available
			# Should only be possible if they chose to wait for a stream
			# and chose only qualities that the streamer ended up not using
			# i.e. 1080p60/720p60 when the stream is only available in 30 FPS
			if not found:
				print("\nThe qualities you selected ended up unavailble for this stream")
				print("You will now have the option to select from the available qualities")
				selected_qualities.clear()
		else:
			aonly = info.quality == VIDEO_LABEL_ITAGS["audio_only"]

			for fmt in formats:
				# Don't bother with refreshing the URL if it's not the kind we can even use
				if not is_fragmented(fmt["url"]):
					continue

				if not aonly and fmt["itag"] == info.quality:
					info.download_urls[DTYPE_VIDEO] = fmt["url"] + "&sq={0}"
				elif fmt["itag"] == AUDIO_ITAG:
					info.download_urls[DTYPE_AUDIO] = fmt["url"] + "&sq={0}"

					if aonly:
						break

		# Grab some extra info on the first run through this function
		if not info.in_progress:
			info.format_info.set_info(player_response)
			info.metadata.set_meta(player_response)
			info.thumbnail = pmfr["thumbnail"]["thumbnails"][0]["url"]
			info.in_progress = True

		info.is_live = is_live
		info.last_updated = time.time()

	return True

# Download a fragment and send it back via data_queue
def download_frags(data_type, info, seq_queue, data_queue):
	downloading = True
	frag_tries = 0
	url = info.download_urls[data_type]
	tname = threading.current_thread().getName()

	while downloading:
		# Check if the user decided to cancel this download, and exit gracefully
		with info.lock:
			if info.stopping:
				break

		tries = 0
		empty_cnt = 0
		bad_url = False
		frag = -1

		try:
			frag = seq_queue.get(timeout=info.target_duration)
			frag_tries = 0
		except queue.Empty:
			# Check again in case the user opted to stop 
			with info.lock:
				if info.stopping:
					downloading = False
					break

			frag_tries += 1
			if frag_tries >= FRAG_MAX_TRIES:
				# For all instances where we might try to stop downloading,
				# make sure the livestream is not still live.
				# If it is, keep trying. Had all video download threads die
				# somehow while a stream was still going. Hopefully this
				# will fix that

				with info.lock:
					if info.active_threads[data_type] > 1:
						logging.debug("{0}: Starved for fragment numbers and multiple fragment threads running".format(tname))
						logging.debug("{0}: Closing this thread to minimize unneeded network requests".format(tname))
						downloading = False
						info.active_threads[data_type] -= 1
						continue

				if info.is_live:
					get_video_info(info)

				if not info.is_live:
					downloading = False
				else:
					logging.debug("{0}: Could not get a new fragment to download after {1} tries and we are the only active downloader".format(tname, FRAG_MAX_TRIES))
					logging.debug("{0}: That is an issue, hopefully it will correct itself".format(tname))
					frag_tries = 0

			continue

		while tries < FRAG_MAX_TRIES and empty_cnt < FRAG_MAX_EMPTY:
			with info.lock:
				if info.stopping:
					downloading = False
					break

			buf = b''

			if bad_url:
				# Check if a new URL is already waiting for us
				# Else refresh auth by calling get_video_info again
				logging.debug("{0}: Attempting to retrieve a new download URL".format(tname))
				with info.lock:
					new_url = info.download_urls[data_type]

					if new_url != url:
						url = new_url
					elif get_video_info(info):
						url = info.download_urls[data_type]

				bad_url = False

			try:
				header_seqnum = -1
				with urllib.request.urlopen(url.format(frag), timeout=info.target_duration * 2) as resp:
					buf = resp.read()
					header_seqnum = int(resp.getheader("X-Head-Seqnum", -1))

				if len(buf) == 0:
					empty_cnt += 1
					if empty_cnt < FRAG_MAX_EMPTY:
						time.sleep(info.target_duration)
					
					continue

				data_queue.put(Fragment(frag, buf, header_seqnum))
				break
			except urllib.error.HTTPError as err:
				logging.debug("{0}: HTTP Error for fragment {1}: {2}".format(tname, frag, err))

				# 403 means our URLs have likely expired
				# Happens every 21540 seconds, or 359 minutes.
				if err.code == 403:
					bad_url = True
				
				tries += 1
				if tries < FRAG_MAX_TRIES:
					time.sleep(info.target_duration)
			except http.client.IncompleteRead as err:
				# Seems to happen on the last chunk that has data. Maybe.
				logging.debug("{0}: Incomplete read on fragment {1}: {2}".format(tname, frag, err))

				tries += 1
				if tries >= FRAG_MAX_TRIES and len(buf) > 0:
					data_queue.put(Fragment(frag, buf, header_seqnum))
					break
				else:
					time.sleep(1)
			except Exception as err:
				logging.debug("{0}: Error with fragment {1}: {2}".format(tname, frag, err))

				tries += 1
				if tries < FRAG_MAX_TRIES:
					time.sleep(1)

			if tries >= FRAG_MAX_TRIES or empty_cnt >= FRAG_MAX_EMPTY:
				logging.debug("{0}: Fragment {1}: {2}/{3} retries; {4}/{5} empty responses".format(
					tname,
					frag,
					tries,
					FRAG_MAX_TRIES,
					empty_cnt,
					FRAG_MAX_EMPTY
				))
				if info.is_live:
					get_video_info(info)

				if not info.is_live:
					downloading = False
				else:
					logging.debug("{0}: Fragment {1}: Stream still live, continuing to download attempt".format(tname, frag))
					tries = 0
					empty_cnt = 0

	logging.debug("{0} exiting".format(tname))

# Download the given data_type stream to dfile
# Sends progress info through progress_queue
def download_stream(data_type, dfile, progress_queue, info):
	data_queue = queue.Queue()
	seq_queue = queue.Queue()
	cur_frag = 0
	cur_seq = 0
	active_downloads = 0
	max_seqs = -1
	tries = 10
	dthreads = []
	data = []
	f = open(dfile, "ab")

	with info.lock:
		for i in range(info.thread_count):
			t = threading.Thread(target=download_frags,
				args=(data_type, info, seq_queue, data_queue),
				name="{0}{1}".format(data_type, i))
			dthreads.append(t)
			info.active_threads[data_type] += 1
			t.start()

	# Initial start
	while active_downloads < info.active_threads[data_type]:
		seq_queue.put(cur_seq)
		cur_seq += 1
		active_downloads += 1

	while True:
		downloading = False

		for t in dthreads:
			if t.is_alive():
				downloading = True
				break

		if not downloading:
			break

		# Get all available data
		while True:
			try:
				d = data_queue.get_nowait()
				data.append(d)
				active_downloads -= 1
			except queue.Empty:
				break

		# Wait for 100ms if no data is available
		if len(data) == 0:
			if active_downloads <= 0:
				logging.debug("Somehow no active downloads and no data to write")

				while active_downloads < info.active_threads[data_type]:
					seq_queue.put(cur_seq)
					cur_seq += 1
					active_downloads += 1

			time.sleep(0.1)
			continue

		# Write any fragments in the queue that are next for writing
		i = 0
		while i < len(data) and tries > 0:
			d = data[i]
			if not d.seq == cur_frag:
				i += 1
				continue

			try:
				b = f.write(d.data)
				cur_frag += 1
				if d.x_head_seqnum > max_seqs:
					max_seqs = d.x_head_seqnum

				progress_queue.put(ProgressInfo(data_type, b, max_seqs))
				data.remove(d)

				# If we know the current max sequence number, use that to
				# determine if we try for another fragment. Else just try anyway
				if max_seqs > 0:
					# One higher than known max as we can download faster than
					# the fragments are made
					if cur_seq <= max_seqs + 1:
						seq_queue.put(cur_seq)
						cur_seq += 1
						active_downloads += 1
				else:
					seq_queue.put(cur_seq) 
					cur_seq += 1
					active_downloads += 1

				tries = 10
				i = 0 # Start from the beginning since the next one might have finished downloading earlier
			except Exception as err:
				tries -= 1
				logging.info("Error when attempting to write fragment {0} to {1}: {2}".format(cur_frag, dfile, err))

				if tries > 0:
					logging.info("Will try {0} more time(s)".format(tries))

		if tries <= 0:
			logging.warning("Stopping download, something must be wrong...")

			with info.lock:
				info.stopping = True

			for t in dthreads:
				t.join()

	if not f.closed:
		f.close()

	logging.debug("{0} download thread closing".format(data_type))

# Find the video ID from the given URL
def get_video_id(url):
	parsedurl = urllib.parse.urlparse(url)
	nl = parsedurl.netloc.lower()
	vid = ""

	if nl == "www.youtube.com" or nl == "youtube.com":
		lpath = parsedurl.path.lower()

		if lpath.startswith("/watch"):
			# parsed queries are always in a list
			parsed_query = urllib.parse.parse_qs(parsedurl.query)

			if not 'v' in parsed_query:
				logging.error("Youtube URL missing video ID")
				return vid

			vid = parsed_query['v'][0]

		# Attempt to find the actual video ID of the current or closest scheduled
		# livestream for a channel
		elif lpath.startswith("/channel") and lpath.endswith("live"):
			# This is fucking awful but it works
			html = download_as_text(url)
			if len(html) == 0:
				return vid

			startidx = html.find(HTML_VIDEO_LINK_TAG)
			if startidx < 0:
				return vid
			
			startidx += len(HTML_VIDEO_LINK_TAG)
			endidx = html.find('"', startidx)
			vid = html[startidx:endidx]
	elif nl == "youtu.be":
		# path includes the leading slash
		vid = parsedurl.path.strip('/')
	else:
		print("{0} is not a known valid youtube URL.".format(url))

	return vid

# Attempt to delete the given file
def try_delete(fname):
	try:
		if os.path.exists(fname):
			logging.info("Deleting file {0}".format(fname))
			os.remove(fname)
	except FileNotFoundError:
		pass
	except Exception as err:
		print("Error deleting file: {0}".format(err))

def print_help():
	fname = os.path.basename(sys.argv[0])

	print()
	print("usage: {0} [OPTIONS] [url] [quality]".format(fname))
	print()

	print("\t[url] is a youtube livestream URL. If not provided, you will be")
	print("\tprompted to enter one.")
	print()

	print("\t[quality] is a slash-delimited list of video qualities you want")
	print("\tto be selected for download, from most to least wanted. If not")
	print("\tprovided, you will be prompted for one, with a list of available")
	print("\tqualities to choose from. The following values are valid:")
	print("\t{0}".format(make_quality_list(VIDEO_LABEL_ITAGS)))
	print()

	print("Options:")
	print("\t-h, --help")
	print("\t\tShow this help message.")
	print()

	print("\t--add-metadata")
	print("\t\tWrite some basic metadata information to the final file.")
	print()

	print("\t-c, --cookies COOKIES_FILE")
	print("\t\tGive a cookies.txt file that has your youtube cookies. Allows")
	print("\t\tthe script to access members-only content if you are a member")
	print("\t\tfor the given stream's user. Must be netscape cookie format.")
	print()

	print("\t-n, --no-wait")
	print("\t\tDo not wait for a livestream if it's a future scheduled stream.")
	print()

	print("\t-o, --output FILENAME_FORMAT")
	print("\t\tSet the output file name EXCLUDING THE EXTENSION. Can include")
	print("\t\tformatting similar to youtube-dl, albeit much more limited.")
	print("\t\tSee FORMAT OPTIONS below for a list of available format keys.")
	print("\t\tDefault is '%(title)s-%(id)s'")
	print()

	print("\t-r, --retry-stream SECONDS")
	print("\t\tIf waiting for a scheduled livestream, re-check if the stream is")
	print("\t\tup every SECONDS instead of waiting for the initial scheduled time.")
	print()

	print("\t--threads THREAD_COUNT")
	print("\t\tSet the number of threads to use for downloading audio and video")
	print("\t\tfragments. The total number of threads running will be")
	print("\t\tTHREAD_COUNT * 2 + 3. Main thread, a thread for each audio and")
	print("\t\tvideo download, and THREAD_COUNT number of fragment downloaders")
	print("\t\tfor both audio and video. The nature of Python means this script")
	print("\t\twill never use more than a single CPU core no matter how many")
	print("\t\tthreads are started. Setting this above 5 is not recommended.")
	print("\t\tDefault is 1.")
	print()

	print("\t-t, --thumbnail")
	print("\t\tDownload and embed the stream thumbnail in the finished file.")
	print("\t\tWhether the thumbnail shows properly depends on your file browser.")
	print("\t\tWindows' seems to work. Nemo on Linux seemingly does not.")
	print()

	print("\t-v, --verbose")
	print("\t\tPrint extra information")
	print()

	print("\t--vp9")
	print("\t\tIf there is a VP9 version of your selected video quality,")
	print("\t\tdownload that instead of the usual h264.")
	print()

	print("\t-w, --wait")
	print("\t\tWait for a livestream if it's a future scheduled stream.")
	print("\t\tIf this option is not used when a scheduled stream is provided,")
	print("\t\tyou will be asked if you want to wait or not.")
	print()

	print("Examples:")
	print("\t{0} -w".format(fname))
	print("\t{0} -w https://www.youtube.com/watch?v=CnWDmKx9cQQ 1080p60/best".format(fname))
	print("\t{0} --threads 3 https://www.youtube.com/watch?v=ZK1GXnz-1Lw best".format(fname))
	print("\t{0} --wait -r 30 https://www.youtube.com/channel/UCZlDXzGoo7d44bwdNObFacg/live best".format(fname))
	print("\t{0} -c cookies-youtube-com.txt https://www.youtube.com/watch?v=_touw1GND-M best".format(fname))
	print("\t{0} --no-wait --add-metadata https://www.youtube.com/channel/UCvaTdHTWBGv3MKj3KVqJVCw/live best".format(fname))
	print("\t{0} -o '%(channel)s/%(upload_date)s_%(title)s' https://www.youtube.com/watch?v=HxV9UAMN12o best".format(fname))
	print()
	print()

	print("FORMAT OPTIONS")
	print("\tFormat keys provided are made to be the same as they would be for")
	print("\tyoutube-dl. See https://github.com/ytdl-org/youtube-dl#output-template")
	print()
	
	print("\tid (string): Video identifier")
	print("\ttitle (string): Video title")
	print("\tchannel_id (string): ID of the channel")
	print("\tchannel (string): Full name of the channel the livestream is on")
	print("\tupload_date (string): Technically stream date (YYYYMMDD)")

def main():
	info = DownloadInfo()
	opts = None
	args = None
	cfile = ""
	fname_format = "%(title)s-%(id)s"
	thumbnail = False
	add_meta = False
	verbose = False
	debug = False

	try:
		opts, args = getopt.getopt(sys.argv[1:],
			"hwntvc:r:o:",
			[
				"help",
				"wait",
				"no-wait",
				"thumbnail",
				"verbose",
				"debug",
				"vp9",
				"add-metadata",
				"cookies=",
				"retry-stream=",
				"output=",
				"threads="
			]
		)
	except getopt.GetoptError as err:
		logging.error("{0}".format(err))
		print_help()
		sys.exit(1)

	for o, a in opts:
		if o in ("-h", "--help"):
			print_help()
			sys.exit(0)
		elif o in ("-w", "--wait"):
			info.wait = WAIT
		elif o in ("-n", "--no-wait"):
			info.wait = NO_WAIT
		elif o in ("-t", "--thumbnail"):
			thumbnail = True
		elif o in ("-v", "--verbose"):
			verbose = True
		elif o == "--vp9":
			info.vp9 = True
		elif o == "--debug":
			debug = True
		elif o == "--add-metadata":
			add_meta = True
		elif o in ("-c", "--cookies"):
			cfile = a
		elif o in ("-o", "--output"):
			fname_format = a
		elif o == "--threads":
			info.thread_count = abs(int(a))
		elif o in ("-r", "--retry-stream"):
			try:
				info.retry_secs = abs(int(a)) # Just abs it, don't bother dealing with negatives
			except Exception:
				logging.error("retry-stream must be given a number argument. Given {0}".format(a))
				sys.exit(1)
		else:
			assert False, "Unhandled option"

	loglevel = logging.WARNING
	if debug:
		loglevel = logging.DEBUG
	elif verbose:
		loglevel = logging.INFO

	logging.basicConfig(format="\r%(asctime)s %(levelname)s:%(message)s{0}{1}".format(" "*25, "\b"*25), datefmt="%H:%M:%S", level=loglevel)

	if len(args) > 1:
		info.url = args[0]
		info.selected_quality = args[1]
	elif len(args) > 0:
		info.url = args[0]
	else:
		info.url = input("Enter a youtube video URL: ")

	info.vid = get_video_id(info.url)
	if not info.vid:
		logging.error("Could not find video ID")
		sys.exit(1)

	# Test filename format to make sure a valid one was given
	try:
		fname_format % info.format_info.get_info()
	except KeyError as err:
		logging.error("Unknown output format key: {0}".format(err))
		sys.exit(1)
	except Exception as err:
		logging.error("Output format test failed: {0}".format(err))
		sys.exit(1)

	# Cookie handling for members-only streams
	if cfile:
		cjar = http.cookiejar.MozillaCookieJar(cfile)
		try:
			cjar.load()
			logging.info("Loaded cookie file {0}".format(cfile))
		except Exception as err:
			logging.error("Failed to load cookies file: {0}".format(err))
			sys.exit(1)
		
		cproc = urllib.request.HTTPCookieProcessor(cjar)
		opener = urllib.request.build_opener(cproc)
		urllib.request.install_opener(opener)

	if not get_video_info(info):
		sys.exit(1)

	try:
		os.mkdir(DTYPE_AUDIO)
	except FileExistsError:
		pass
	except Exception as err:
		logging.error("Failed to create audio dir: {0}".format(err))
		sys.exit(1)
	
	try:
		os.mkdir(DTYPE_VIDEO)
	except FileExistsError:
		pass
	except Exception as err:
		logging.error("Failed to create video dir: {0}".format(err))
		sys.exit(1)

	# Setup file name and directories
	full_fpath = fname_format % info.format_info.get_info()
	fdir = os.path.dirname(full_fpath)
	fname = os.path.basename(full_fpath)
	fname = sterilize_filename(fname)

	if len(fname.strip()) == 0:
		logging.error("Output file name appears to be empty.")
		logging.error("Expanded output file path: {0}".format(full_fpath))
		sys.exit(1)

	afile = os.path.join(DTYPE_AUDIO, "{0}.f{1}.ts".format(fname, AUDIO_ITAG))
	vfile = os.path.join(DTYPE_VIDEO, "{0}.f{1}.ts".format(fname, info.quality))
	thmbnl_file = os.path.join(DTYPE_VIDEO, "{0}.jpeg".format(fname))

	progress_queue = queue.Queue()
	total_bytes = 0
	threads = []
	frags = {
		DTYPE_AUDIO: 0,
		DTYPE_VIDEO: 0
	}

	# Grab the thumbnail for the livestream for embedding later
	if thumbnail and info.thumbnail:
		thumbnail = download_thumbnail(info.thumbnail, thmbnl_file)

		# Failed to download but file itself got created. Remove it
		if not thumbnail and os.path.exists(thmbnl_file):
			try_delete(thmbnl_file)

	
	logging.info("Starting download to {0}".format(afile))
	athread = threading.Thread(target=download_stream, args=(DTYPE_AUDIO, afile, progress_queue, info))
	threads.append(athread)
	athread.start()

	if info.download_urls[DTYPE_VIDEO]:
		logging.info("Starting download to {0}".format(vfile))
		vthread = threading.Thread(target=download_stream, args=(DTYPE_VIDEO, vfile, progress_queue, info))
		threads.append(vthread)
		vthread.start()

	# Print progress to stdout
	# Included info is video and audio fragments downloaded, and total data downloaded
	max_seqs = -1
	while True:
		alive = False

		for t in threads:
			if t.is_alive():
				alive = True
				break

		try:
			progress = progress_queue.get(timeout=1)
			total_bytes += progress.bytes
			frags[progress.data_type] += 1

			if progress.max_seq > max_seqs:
				max_seqs = progress.max_seq

			status = "\rVideo fragments: {0}; Audio fragments: {1}; ".format(frags[DTYPE_VIDEO], frags[DTYPE_AUDIO])
			if debug:
				status += "Max sequence: {0}; ".format(max_seqs)
			
			status += "Total Downloaded: {0}{1}{2}".format(format_size(total_bytes), " "*5, "\b"*5)
			print(status, end="")
		except queue.Empty:
			pass
		except KeyboardInterrupt:
			# Attempt to shutdown gracefully by stopping the download threads
			with info.lock:
				info.stopping = True
			print("\nKeyboard Interrupt, stopping download...")

			for t in threads:
				t.join()

			merge = get_yes_no("Download stopped prematurely. Would you like to merge the currently downloaded data? [y/N]: ")
			if merge:
				alive = False
			else:
				sys.exit(2)
		
		if not alive:
			break

	print()
	aonly = info.quality == VIDEO_LABEL_ITAGS["audio_only"]

	# Attempt to mux the video and audio files using ffmpeg
	if not aonly and frags[DTYPE_AUDIO] != frags[DTYPE_VIDEO]:
		print("Mismatched number of video and audio fragments.")
		print("The files should still be mergable but data might be missing somewhere.")

	ffmpeg = shutil.which("ffmpeg")
	if not ffmpeg:
		print("ffmpeg not found. Please install ffmpeg.")

		if aonly:
			print("To place the audio in its proper container, run the following command:")
			print("ffmpeg -i {0} -c copy {1}.m4a".format(afile, fname))
		else:
			print("To merge the files, run the following command:")
			print("ffmpeg -i {0} -i {1} -c copy {2}.mp4".format(vfile, afile, fname))
			
		sys.exit(0)

	retcode = 0
	mfile = ""

	# Output format included a directory structure. Create it if it doesn't exist
	if fdir:
		try:
			os.makedirs(fdir, exist_ok=True)
		except Exception as err:
			print("Error creating final file directory: {0}".format(err))
			print("The final file will be placed in the current working directory")
			fdir = ""

	ffmpeg_args = [
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "fatal",
		"-i", afile
	]

	if thumbnail:
		ffmpeg_args.extend(["-i", thmbnl_file])

	if aonly:
		logging.info("Correcting audio container")
		mfile = os.path.join(fdir, "{0}.m4a".format(fname))
	else:
		logging.info("Muxing files")
		mfile = os.path.join(fdir, "{0}.mp4".format(fname))

		ffmpeg_args.extend(["-i", vfile])
		if thumbnail:
			ffmpeg_args.extend([
				"-map", "0",
				"-map", "1",
				"-map", "2"
			])
		
	ffmpeg_args.extend(["-c", "copy"])
	if thumbnail:
		ffmpeg_args.extend(["-disposition:v:0", "attached_pic"])

	if add_meta:
		for k, v in info.metadata.get_meta().items():
			ffmpeg_args.extend([
				"-metadata",
				"{0}={1}".format(k.upper(), v)
			])
	
	ffmpeg_args.append(mfile)
	retcode = execute(ffmpeg_args)

	if retcode != 0:
		print("execute returned code {0}. Something must have gone wrong with ffmpeg.".format(retcode))
		sys.exit(retcode)

	try_delete(afile)
	try_delete(vfile)
	if thumbnail:
		try_delete(thmbnl_file)

	print()
	print("Final file: {0}".format(mfile))

if __name__ == "__main__":
	main()