package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	PlayableOk         = "OK"
	PlayableOffline    = "LIVE_STREAM_OFFLINE"
	PlayableUnplayable = "UNPLAYABLE"
	PlayableError      = "ERROR"

	AndroidAPIPostData = `{
	'context': {
		'client': {
			'clientName': 'ANDROID',
			'clientVersion': '19.09.37',
			'hl': 'en'
		}
	},
	'videoId': '%s',
	'params': 'CgIQBg==',
	'playbackContext': {
		'contentPlaybackContext': {
			'html5Preference': 'HTML5_PREF_WANTS'
		}
	},
	'contentCheckOk': true,
	'racyCheckOk': true
}
	`
)

const (
	PlayerResponseFound = iota
	PlayerResponseNotFound
	PlayerResponseNotUsable
)

var (
	playerRespDecl    = []byte("var ytInitialPlayerResponse =")
	ytInitialDataDecl = []byte("var ytInitialData =")
)

/*
Auto-generated using https://mholt.github.io/json-to-go/
Trimmed after to relevent fields
*/
type PlayerResponse struct {
	ResponseContext struct {
		MainAppWebResponseContext struct {
			LoggedOut bool `json:"loggedOut"`
		} `json:"mainAppWebResponseContext"`
	} `json:"responseContext"`
	PlayabilityStatus struct {
		Status            string `json:"status"`
		Reason            string `json:"reason"`
		LiveStreamability struct {
			LiveStreamabilityRenderer struct {
				VideoID      string `json:"videoId"`
				OfflineSlate struct {
					LiveStreamOfflineSlateRenderer struct {
						ScheduledStartTime string `json:"scheduledStartTime"`
					} `json:"liveStreamOfflineSlateRenderer"`
				} `json:"offlineSlate"`
				PollDelayMs string `json:"pollDelayMs"`
			} `json:"liveStreamabilityRenderer"`
		} `json:"liveStreamability"`
	} `json:"playabilityStatus"`
	StreamingData struct {
		ExpiresInSeconds string `json:"expiresInSeconds"`
		AdaptiveFormats  []struct {
			Itag              int     `json:"itag"`
			URL               string  `json:"url"`
			MimeType          string  `json:"mimeType"`
			QualityLabel      string  `json:"qualityLabel,omitempty"`
			TargetDurationSec float64 `json:"targetDurationSec"`
		} `json:"adaptiveFormats"`
		DashManifestURL string `json:"dashManifestUrl"`
	} `json:"streamingData"`
	VideoDetails struct {
		VideoID          string  `json:"videoId"`
		Title            string  `json:"title"`
		LengthSeconds    string  `json:"lengthSeconds"`
		IsLive           bool    `json:"isLive"`
		ChannelID        string  `json:"channelId"`
		IsOwnerViewing   bool    `json:"isOwnerViewing"`
		ShortDescription string  `json:"shortDescription"`
		AverageRating    float64 `json:"averageRating"`
		AllowRatings     bool    `json:"allowRatings"`
		ViewCount        string  `json:"viewCount"`
		Author           string  `json:"author"`
		IsLiveContent    bool    `json:"isLiveContent"`
	} `json:"videoDetails"`
	Microformat struct {
		PlayerMicroformatRenderer struct {
			Thumbnail struct {
				Thumbnails []struct {
					URL string `json:"url"`
				} `json:"thumbnails"`
			} `json:"thumbnail"`
			LiveBroadcastDetails struct {
				IsLiveNow      bool   `json:"isLiveNow"`
				StartTimestamp string `json:"startTimestamp"`
				EndTimestamp   string `json:"endTimestamp"`
			} `json:"liveBroadcastDetails"`
			PublishDate string `json:"publishDate"`
			UploadDate  string `json:"uploadDate"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`
}

type YtInitialData struct {
	Contents struct {
		Twocolumnbrowseresultsrenderer struct {
			Tabs []struct {
				Tabrenderer struct {
					Endpoint struct {
						Commandmetadata struct {
							Webcommandmetadata struct {
								URL string `json:"url"`
							} `json:"webCommandMetadata"`
						} `json:"commandMetadata"`
					} `json:"endpoint"`
					Content struct {
						Richgridrenderer struct {
							Contents []RichGridContent `json:"contents"`
						} `json:"richGridRenderer"`
					} `json:"content"`
				} `json:"tabRenderer"`
			} `json:"tabs"`
		} `json:"twoColumnBrowseResultsRenderer"`
	} `json:"contents"`
}

type RichGridContent struct {
	Richitemrenderer struct {
		Content struct {
			Videorenderer struct {
				Videoid           string `json:"videoId"`
				Thumbnailoverlays []struct {
					Thumbnailoverlaytimestatusrenderer struct {
						Style string `json:"style"`
					} `json:"thumbnailOverlayTimeStatusRenderer"`
				} `json:"thumbnailOverlays"`
				Badges []struct {
					Metadatabadgerenderer struct {
						Style string `json:"style"`
					} `json:"metadataBadgeRenderer"`
				} `json:"badges"`
			} `json:"videoRenderer"`
		} `json:"content"`
	} `json:"richItemRenderer"`
}

// Search the given HTML for the player response object
func GetJsonFromHtml(htmlData []byte, jsonDecl []byte) []byte {
	var objData []byte
	reader := bytes.NewReader(htmlData)
	tokenizer := html.NewTokenizer(reader)
	isScript := false

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return objData
		case html.TextToken:
			if isScript {
				data := tokenizer.Text()
				declStart := bytes.Index(data, jsonDecl)
				if declStart < 0 {
					continue
				}

				// Maybe add a LogTrace in the future for stuff like this
				//LogDebug("Found script element with player response in watch page.")
				objStart := bytes.Index(data[declStart:], []byte("{")) + declStart
				objEnd := bytes.LastIndex(data[objStart:], []byte("};")) + 1 + objStart

				if objEnd > objStart {
					objData = data[objStart:objEnd]
				}

				return objData
			}
		case html.StartTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "script" {
				isScript = true
			} else {
				isScript = false
			}
		}
	}
}

func (di *DownloadInfo) GetNewestStreamFromStreams() string {
	// Surely there won't be more than 5 simultaneous streams when looking for membership streams, right?
	const MAX_STREAM_ITEM_CHECK = 5
	streamUrl := ""
	if !di.LiveURL {
		return streamUrl
	}

	initialData := &YtInitialData{}
	var contents []RichGridContent
	streamsUrl := strings.Replace(di.URL, "/live", "/streams", 1)
	streamsHtml := DownloadData(streamsUrl)
	ytInitialData := GetJsonFromHtml(streamsHtml, ytInitialDataDecl)

	err := json.Unmarshal(ytInitialData, initialData)
	if err != nil {
		return streamUrl
	}

	for _, tab := range initialData.Contents.Twocolumnbrowseresultsrenderer.Tabs {
		if strings.HasSuffix(tab.Tabrenderer.Endpoint.Commandmetadata.Webcommandmetadata.URL, "/streams") {
			contents = tab.Tabrenderer.Content.Richgridrenderer.Contents
		}
	}

	for i, content := range contents {
		if i >= MAX_STREAM_ITEM_CHECK {
			break
		}

		videoRenderer := content.Richitemrenderer.Content.Videorenderer
		if di.MembersOnly {
			mengen := false
			for _, badge := range videoRenderer.Badges {
				if badge.Metadatabadgerenderer.Style == "BADGE_STYLE_TYPE_MEMBERS_ONLY" {
					mengen = true
					break
				}
			}

			if !mengen {
				continue
			}
		}

		for _, thumbnailRenderer := range videoRenderer.Thumbnailoverlays {
			if thumbnailRenderer.Thumbnailoverlaytimestatusrenderer.Style == "LIVE" {
				streamUrl = fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoRenderer.Videoid)
				return streamUrl
			}
		}
	}

	return streamUrl
}

// At the time of adding, retrieving the player response from the api while
// claiming to be the android client seems to result in unthrottled download
// URLs. Credit to yt-dlp devs for POST data and headers.
func (di *DownloadInfo) DownloadAndroidPlayerResponse() (*PlayerResponse, error) {
	pr := &PlayerResponse{}
	auth := GenerateSAPISIDHash(di.CookiesURL)
	data := []byte(fmt.Sprintf(AndroidAPIPostData, di.VideoID))
	req, err := http.NewRequest("POST", "https://www.youtube.com/youtubei/v1/player?key=AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8", bytes.NewBuffer(data))

	if err != nil {
		return nil, err
	}

	req.Header.Add("X-YouTube-Client-Name", "3")
	req.Header.Add("X-YouTube-Client-Version", "19.09.37")
	req.Header.Add("Origin", "https://www.youtube.com")
	req.Header.Add("content-type", "application/json")

	if len(auth) > 0 {
		req.Header.Add("X-Origin", "https://www.youtube.com")
		req.Header.Add("Authorization", auth)
	}

	if di.Ytcfg != nil {
		if len(di.Ytcfg.IdToken) > 0 {
			req.Header.Add("X-Youtube-Identity-Token", di.Ytcfg.IdToken)
		}

		if len(di.Ytcfg.DelegatedSessionId) > 0 {
			req.Header.Add("X-Goog-PageId", di.Ytcfg.DelegatedSessionId)
		}

		if len(di.Ytcfg.VisitorData) > 0 {
			req.Header.Add("X-Goog-Visitor-Id", di.Ytcfg.VisitorData)
		}

		if len(di.Ytcfg.SessionIndex) > 0 {
			req.Header.Add("X-Goog-AuthUser", di.Ytcfg.SessionIndex)
		}
	}

	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned non-200 status code %d", resp.StatusCode)
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(respData, pr)
	if err != nil {
		return nil, err
	}

	return pr, nil
}

func (di *DownloadInfo) GetVideoHtml() []byte {
	var videoHtml []byte

	if di.LiveURL {
		streamUrl := di.GetNewestStreamFromStreams()

		if len(streamUrl) > 0 {
			videoHtml = DownloadData(streamUrl)
		}
	}

	if len(videoHtml) == 0 && !di.MembersOnly {
		videoHtml = DownloadData(di.URL)
	}

	return videoHtml
}

// Get the player response object from youtube
func (di *DownloadInfo) GetPlayerResponse(videoHtml []byte) (*PlayerResponse, error) {
	pr := &PlayerResponse{}

	if len(videoHtml) == 0 {
		return nil, fmt.Errorf("unable to retrieve data from video page")
	}

	prData := GetJsonFromHtml(videoHtml, playerRespDecl)
	if len(prData) == 0 {
		if debug && di.InProgress {
			LogDebug("Could not find player response from video watch page. Writing html file to %s.html", di.VideoID)
			os.WriteFile(fmt.Sprintf("%s.html", di.VideoID), videoHtml, di.FileMode)
		}
		return nil, fmt.Errorf("unable to retrieve player response object from watch page")
	}

	err := json.Unmarshal(prData, pr)
	if err != nil {
		return nil, err
	}

	if di.LiveURL {
		videoId := GetVideoIdFromWatchPage(videoHtml)
		if len(videoId) > 0 {
			di.VideoID = videoId
		}
	}

	return pr, nil
}

func (di *DownloadInfo) GetPlayablePlayerResponse() (retrieved int, pr *PlayerResponse, selectedQualities []string) {
	firstWait := true
	isLiveURL := di.LiveURL
	waitOnLiveURL := isLiveURL && di.RetrySecs > 0 && !di.InProgress
	liveWaited := 0
	retryCount := 0
	var secsLate int
	var err error

	if len(di.SelectedQuality) > 0 {
		selectedQualities = ParseQualitySelection(VideoQualities, di.SelectedQuality)
	}

	for {
		videoHtml := di.GetVideoHtml()
		pr, err = di.GetPlayerResponse(videoHtml)

		if err != nil {
			if waitOnLiveURL {
				if len(selectedQualities) < 1 {
					fmt.Fprintln(os.Stderr)
					selectedQualities = GetQualityFromUser(VideoQualities, true)
				}

				if liveWaited == 0 {
					LogGeneral("You have opted to wait for a livestream to be scheduled. Retrying every %d seconds.\n", di.RetrySecs)
				}

				time.Sleep(time.Duration(di.RetrySecs) * time.Second)
				liveWaited += di.RetrySecs
				retryCount += 1
				if loglevel > LoglevelQuiet {
					msg := "Retries: %d (Last retry: %s), Total time waited: %d seconds"
					if !statusNewlines {
						msg = "\r" + msg
					} else {
						msg = msg + "\n"
					}

					fmt.Fprintf(os.Stderr, msg,
						retryCount,
						time.Now().Format("2006/01/02 15:04:05"),
						liveWaited,
					)
				}
				continue
			}

			fmt.Fprintln(os.Stderr)
			LogError("Error retrieving player response: %s", err.Error())
			return PlayerResponseNotFound, nil, nil
		}

		if len(pr.VideoDetails.VideoID) == 0 {
			if di.InProgress {
				LogWarn("Video details no longer available mid download.")
				LogWarn("Stream was likely privated after finishing.")
				LogWarn("We will continue to download, but if it starts to fail, nothing can be done.")
				di.printStatusWithoutLock()
			}

			LogError("Video Details not found, video is likely private or does not exist.")
			di.Live = false
			di.Unavailable = true

			return PlayerResponseNotUsable, nil, nil
		}

		if len(pr.PlayabilityStatus.LiveStreamability.LiveStreamabilityRenderer.VideoID) == 0 && !pr.VideoDetails.IsLiveContent {
			if di.Live {
				di.Live = false
			} else {
				LogError("%s is not a livestream. It would be better to use yt-dlp to download it.", di.URL)
			}

			return PlayerResponseNotUsable, nil, nil
		}

		switch pr.PlayabilityStatus.Status {
		case PlayableError:
			if di.InProgress {
				LogInfo("Finishing download")
			}

			LogError("Playability status: ERROR. Reason: %s", pr.PlayabilityStatus.Reason)
			di.Live = false

			return PlayerResponseNotUsable, nil, nil

		case PlayableUnplayable:
			loggedIn := !pr.ResponseContext.MainAppWebResponseContext.LoggedOut

			LogError("Playability status: UNPLAYABLE.")
			LogError("Reason: %s", pr.PlayabilityStatus.Reason)
			LogError("Logged in status: %t", loggedIn)
			LogError("If this is a members only stream, you provided a cookies.txt file, and the above 'logged in' status is not True, please try updating your cookies file.")

			di.Unavailable = true
			if di.InProgress {
				di.printStatusWithoutLock()
			}

			return PlayerResponseNotUsable, nil, nil

		case PlayableOffline:
			if di.InProgress {
				LogDebug("Livestream status is %s mid-download", PlayableOffline)
				return PlayerResponseNotUsable, nil, nil
			}

			if di.Wait == ActionDoNot {
				LogError("Stream has not started, and you have opted not to wait.")
				return PlayerResponseNotUsable, nil, nil
			}

			if firstWait && di.Wait == ActionAsk && di.RetrySecs == 0 {
				if !di.AskWaitForStream() {
					return PlayerResponseNotUsable, nil, nil
				}
			}

			if firstWait {
				if !(isLiveURL && di.RetrySecs > 0) {
					di.printChannelAndTitle(pr)
				}
				fmt.Fprintln(os.Stderr)
				if len(selectedQualities) < 1 {
					selectedQualities = GetQualityFromUser(VideoQualities, true)
				}
			}

			if di.RetrySecs > 0 {
				if firstWait {
					firstWait = false
					LogGeneral("Waiting for stream, retrying every %d seconds...\n", di.RetrySecs)
				}

				time.Sleep(time.Duration(di.RetrySecs) * time.Second)
				liveWaited += di.RetrySecs
				retryCount += 1
				if loglevel > LoglevelQuiet {
					msg := "Retries: %d (Last retry: %s), Total time waited: %d seconds"
					if !statusNewlines {
						msg = "\r" + msg
					} else {
						msg = msg + "\n"
					}

					fmt.Fprintf(os.Stderr, msg,
						retryCount,
						time.Now().Format("2006/01/02 15:04:05"),
						liveWaited,
					)
				}
				continue
			}

			schedTime, err := strconv.ParseInt(pr.PlayabilityStatus.LiveStreamability.LiveStreamabilityRenderer.OfflineSlate.LiveStreamOfflineSlateRenderer.ScheduledStartTime,
				10, 64)
			if err != nil {
				LogWarn("Failed to get stream start time: %s.", err)
				LogWarn("Falling back to polling.")
				di.RetrySecs = DefaultPollTime
				time.Sleep(time.Duration(di.RetrySecs) * time.Second)
				continue
			}

			curTime := time.Now().Unix()
			slepTime := schedTime - curTime

			if slepTime > 0 {
				if !firstWait {
					LogGeneral("Stream rescheduled.")
				}

				firstWait = false
				secsLate = 0

				LogGeneral("Stream starts at %s in %d seconds. ",
					pr.Microformat.PlayerMicroformatRenderer.LiveBroadcastDetails.StartTimestamp,
					slepTime)
				LogGeneral("Waiting for this time to elapse...")

				// Loop it just in case a rogue sleep interrupt happens
				for slepTime > 0 {
					time.Sleep(time.Duration(slepTime) * time.Second)
					curTime = time.Now().Unix()
					slepTime = schedTime - curTime

					if slepTime > 0 {
						LogDebug("Woke up %d seconds early. Continuing sleep...", slepTime)
					}
				}

				// We've waited until the scheduled time
				continue
			}

			if firstWait {
				LogGeneral("Stream should have started. Checking back every %d seconds\n", DefaultPollTime)
				firstWait = false
			}

			/*
				If we get this far, the stream's scheduled time has passed but it's still not started
				Check every 15 seconds
			*/
			time.Sleep(time.Duration(DefaultPollTime) * time.Second)
			secsLate += DefaultPollTime
			LogGeneral("Stream is %d seconds late...", secsLate)
			continue

		case PlayableOk:
			// player response returned from /live does not include full information
			if isLiveURL {
				di.URL = fmt.Sprintf("https://www.youtube.com/watch?v=%s", di.VideoID)
				di.MembersOnly = false
				isLiveURL = false
				continue
			}

			di.printChannelAndTitle(pr)
			streamData := pr.StreamingData
			liveDetails := pr.Microformat.PlayerMicroformatRenderer.LiveBroadcastDetails
			isLive := liveDetails.IsLiveNow

			if !isLive && !di.InProgress {
				/*
					The livestream has likely ended already.
					Check if the stream has been processed.
					If not, then download it.
				*/
				if len(liveDetails.EndTimestamp) > 0 {
					if len(streamData.AdaptiveFormats) > 0 {
						// Assume that all formats will be fully processed if one is, and vice versa
						if len(streamData.AdaptiveFormats[0].URL) == 0 {
							LogGeneral("Livestream has ended and is being processed. Download URLs not available.")
							return PlayerResponseNotUsable, nil, nil
						}

						if !IsFragmented(streamData.AdaptiveFormats[0].URL) {
							LogGeneral("Livestream has been processed. Use yt-dlp instead.")
							return PlayerResponseNotUsable, nil, nil
						}
					} else {
						LogGeneral("Livestream has ended and is being processed. Download URLs not available.")
						return PlayerResponseNotUsable, nil, nil
					}
				} else {
					/*
						I actually ran into this case once so far.
						Stream is set as playable, but has not started.
					*/
					LogGeneral("Livestream is offline, should have started, and does not have an end timestamp.")
					LogGeneral("Waiting %d seconds and trying again.\n", DefaultPollTime)
					time.Sleep(time.Duration(DefaultPollTime) * time.Second)
					continue
				}
			}

			err = di.GetYTCFG(videoHtml)
			if err != nil {
				LogDebug("Error getting ytcfg: %s", err.Error())
			}
		default:
			if secsLate > 0 {
				fmt.Fprintln(os.Stderr)
			}

			LogError("Unknown playability status: %s", pr.PlayabilityStatus.Status)
			if di.InProgress {
				di.Live = false
			}

			return PlayerResponseNotUsable, nil, nil
		}

		if secsLate > 0 {
			fmt.Fprintln(os.Stderr)
		}

		break
	}

	return PlayerResponseFound, pr, selectedQualities
}
