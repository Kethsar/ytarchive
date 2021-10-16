package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"golang.org/x/net/html"
)

var ytcfgStart = []byte("ytcfg.set(")

// TODO: If necessary, grab dataSyncIds as well
// Will be needed if SessionIndex is not available
type YTCFG struct {
	DelegatedSessionId     string `json:"DELEGATED_SESSION_ID"`
	IdToken                string `json:"ID_TOKEN"`
	Hl                     string `json:"HL"`
	InnertubeApiKey        string `json:"INNERTUBE_API_KEY"`
	InnertubeClientName    string `json:"INNERTUBE_CLIENT_NAME"`
	InnertubeClientVersion string `json:"INNERTUBE_CLIENT_VERSION"`
	InnertubeCtxClientName int    `json:"INNERTUBE_CONTEXT_CLIENT_NAME"`
	SessionIndex           string `json:"SESSION_INDEX"`
	VisitorData            string `json:"VISITOR_DATA"`
}

// Search the given HTML for the ytcfg object
func GetYTCFGFromHtml(data []byte) []byte {
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
				setStart := bytes.Index(data, ytcfgStart)
				if setStart < 0 {
					continue
				}

				// Maybe add a LogTrace in the future for stuff like this
				//LogDebug("Found script element with ytcfg data in watch page.")
				objStart := bytes.Index(data[setStart:], []byte("{")) + setStart
				objEnd := bytes.Index(data[objStart:], []byte("});")) + 1 + objStart

				if objEnd > objStart {
					objData = data[objStart:objEnd]
				}

				if len(objData) > 0 {
					return objData
				}
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

func (di *DownloadInfo) GetYTCFG(videoHtml []byte) error {
	ytcfg := &YTCFG{}

	if len(videoHtml) == 0 {
		return fmt.Errorf("unable to retrieve data from video page")
	}

	prData := GetYTCFGFromHtml(videoHtml)
	if len(prData) == 0 {
		return fmt.Errorf("unable to retrieve ytcfg data from watch page")
	}

	err := json.Unmarshal(prData, ytcfg)
	if err != nil {
		return err
	}

	di.Ytcfg = ytcfg

	return nil
}
