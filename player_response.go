package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
			'clientVersion': '16.20',
			'hl': 'en'
		}
	},
	'videoId': '%s',
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
	playerRespDecl = []byte("var ytInitialPlayerResponse =")
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

// Search the given HTML for the player response object
func GetPlayerResponseFromHtml(data []byte) []byte {
	var objData []byte
	reader := bytes.NewReader(data)
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
				declStart := bytes.Index(data, playerRespDecl)
				if declStart < 0 {
					continue
				}

				// Maybe add a LogTrace in the future for stuff like this
				//LogDebug("Found script element with player response in watch page.")
				objStart := bytes.Index(data[declStart:], []byte("{")) + declStart
				objEnd := bytes.Index(data[objStart:], []byte("};")) + 1 + objStart

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
	req.Header.Add("X-YouTube-Client-Version", "16.20")
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

// Get the player response object from youtube
func (di *DownloadInfo) GetPlayerResponse(videoHtml []byte) (*PlayerResponse, error) {
	pr := &PlayerResponse{}

	if len(videoHtml) == 0 {
		return nil, fmt.Errorf("unable to retrieve data from video page")
	}

	prData := GetPlayerResponseFromHtml(videoHtml)
	if len(prData) == 0 {
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
	waitOnLiveURL := isLiveURL && di.RetrySecs > 0
	liveWaited := 0
	var secsLate int
	var err error

	if len(di.SelectedQuality) > 0 {
		selectedQualities = ParseQualitySelection(VideoQualities, di.SelectedQuality)
	}

	for {
		videoHtml := DownloadData(di.URL)
		pr, err = di.GetPlayerResponse(videoHtml)

		if err != nil {
			if waitOnLiveURL {
				if liveWaited == 0 {
					fmt.Printf("\nYou have opted to wait for a livestream to be scheduled. Waiting every %d seconds.\n", di.RetrySecs)
				}

				time.Sleep(time.Duration(di.RetrySecs) * time.Second)
				liveWaited += di.RetrySecs
				fmt.Printf("\rTotal time waited: %d seconds", liveWaited)
				continue
			}

			fmt.Println()
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
			LogError("Also check if your cookies file includes '#HttpOnly_' in front of some lines. If it does, delete that part of those lines and try again.")

			di.Live = false
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
				fmt.Println()
				if len(selectedQualities) < 1 {
					selectedQualities = GetQualityFromUser(VideoQualities, true)
				}
			}

			if di.RetrySecs > 0 {
				if firstWait {
					firstWait = false
					fmt.Printf("Waiting for stream, retrying every %d seconds...\n", di.RetrySecs)
				}

				time.Sleep(time.Duration(di.RetrySecs) * time.Second)
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
					fmt.Println("\nStream rescheduled.")
				}

				firstWait = false
				secsLate = 0

				fmt.Printf("Stream starts at %s in %d seconds. ",
					pr.Microformat.PlayerMicroformatRenderer.LiveBroadcastDetails.StartTimestamp,
					slepTime)
				fmt.Println("Waiting for this time to elapse...")

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
				fmt.Printf("Stream should have started. Checking back every %d seconds\n", DefaultPollTime)
				firstWait = false
			}

			/*
				If we get this far, the stream's scheduled time has passed but it's still not started
				Check every 15 seconds
			*/
			time.Sleep(time.Duration(DefaultPollTime) * time.Second)
			secsLate += DefaultPollTime
			fmt.Printf("\rStream is %d seconds late...", secsLate)
			continue

		case PlayableOk:
			// player response returned from /live does not include full information
			if isLiveURL {
				di.URL = fmt.Sprintf("https://www.youtube.com/watch?v=%s", di.VideoID)
				isLiveURL = false
				continue
			}

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
							fmt.Println("Livestream has ended and is being processed. Download URLs not available.")
							return PlayerResponseNotUsable, nil, nil
						}

						if !IsFragmented(streamData.AdaptiveFormats[0].URL) {
							fmt.Println("Livestream has been processed. Use yt-dlp instead.")
							return PlayerResponseNotUsable, nil, nil
						}
					} else {
						fmt.Println("Livestream has ended and is being processed. Download URLs not available.")
						return PlayerResponseNotUsable, nil, nil
					}
				} else {
					/*
						I actually ran into this case once so far.
						Stream is set as playable, but has not started.
					*/
					fmt.Println("Livestream is offline, should have started, and does not have an end timestamp.")
					fmt.Printf("Waiting %d seconds and trying again.\n", DefaultPollTime)
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
				fmt.Println()
			}

			LogError("Unknown playability status: %s", pr.PlayabilityStatus.Status)
			if di.InProgress {
				di.Live = false
			}

			return PlayerResponseNotUsable, nil, nil
		}

		if secsLate > 0 {
			fmt.Println()
		}

		break
	}

	return PlayerResponseFound, pr, selectedQualities
}
