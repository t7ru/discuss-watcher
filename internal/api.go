package watcher

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// apiClient talks to WeShallDiscuss's Fandom-compatible endpoint under /wikia.php
type apiClient struct {
	base   string
	client *http.Client
}

type postsResponse struct {
	Embedded struct {
		Posts []*post `json:"doc:posts"`
	} `json:"_embedded"`
}

func (a *apiClient) getPosts(ctx context.Context, limit int) ([]*post, error) {
	var out postsResponse
	err := a.get(ctx, "/wikia.php", url.Values{
		"controller":      {"DiscussionPost"},
		"method":          {"getPosts"},
		"sortKey":         {"creation_date"},
		"sortDirection":   {"descending"},
		"includeCounters": {"false"},
		"limit":           {strconv.Itoa(limit)},
	}, &out)
	return out.Embedded.Posts, err
}

func (a *apiClient) getArticleNames(ctx context.Context, ids []string) (map[string]articleName, error) {
	var out struct {
		ArticleNames map[string]articleName `json:"articleNames"`
	}
	err := a.get(ctx, "/wikia.php", url.Values{
		"controller":    {"FeedsAndPosts"},
		"method":        {"getArticleNamesAndUsernames"},
		"stablePageIds": {strings.Join(ids, ",")},
		"format":        {"json"},
	}, &out)
	return out.ArticleNames, err
}

func (a *apiClient) getArticlePath(ctx context.Context) (string, error) {
	var out struct {
		Query struct {
			General struct {
				ArticlePath string `json:"articlepath"`
			} `json:"general"`
		} `json:"query"`
	}
	err := a.get(ctx, "/api.php", url.Values{
		"action": {"query"},
		"meta":   {"siteinfo"},
		"siprop": {"general"},
		"format": {"json"},
	}, &out)
	path := out.Query.General.ArticlePath
	if err != nil {
		return "", err
	}
	if !strings.Contains(path, "$1") {
		return "", fmt.Errorf("unexpected articlepath %q", path)
	}
	return path, nil
}

func (a *apiClient) get(ctx context.Context, endpoint string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/hal+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("wikia.php %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.UnmarshalRead(resp.Body, out)
}

type articleName struct {
	Title       string `json:"title"`
	RelativeURL string `json:"relativeUrl"`
}

type user struct {
	ID        string `json:"id"`
	AvatarURL string `json:"avatarUrl"`
	Name      string `json:"name"`
}

type epoch struct {
	EpochSecond int64 `json:"epochSecond"`
	Nano        int64 `json:"nano"`
}

type pollImage struct {
	URL string `json:"url"`
}

type pollAnswer struct {
	Text  string     `json:"text"`
	Image *pollImage `json:"image"`
}

type poll struct {
	Answers []pollAnswer `json:"answers"`
}

type tag struct {
	ArticleTitle string `json:"articleTitle"`
	RelativeURL  string `json:"relativeUrl"`
}

type threadInfo struct {
	ContainerType string `json:"containerType"`
	Title         string `json:"title"`
	Tags          []tag  `json:"tags"`
}

type postEmbedded struct {
	Thread []threadInfo `json:"thread"`
}

// api returns the same shape for opening posts and replies
// IsReply tells them apart
type post struct {
	ID           string       `json:"id"`
	ThreadID     string       `json:"threadId"`
	CreatorID    string       `json:"creatorId"`
	CreatorIP    string       `json:"creatorIp"`
	ForumID      string       `json:"forumId"`
	ForumName    string       `json:"forumName"`
	Title        string       `json:"title"`
	IsReply      bool         `json:"isReply"`
	Funnel       string       `json:"funnel"`
	JsonModel    string       `json:"jsonModel"`
	RawContent   string       `json:"rawContent"`
	CreationDate epoch        `json:"creationDate"`
	CreatedBy    user         `json:"createdBy"`
	Poll         *poll        `json:"poll"`
	Embedded     postEmbedded `json:"_embedded"`
}

func (p *post) containerType() string {
	if len(p.Embedded.Thread) > 0 {
		return p.Embedded.Thread[0].ContainerType
	}
	return ""
}

func (p *post) threadTitle() string {
	if len(p.Embedded.Thread) > 0 && p.Embedded.Thread[0].Title != "" {
		return p.Embedded.Thread[0].Title
	}
	return p.Title
}

func (p *post) threadTags() []tag {
	if len(p.Embedded.Thread) > 0 {
		return p.Embedded.Thread[0].Tags
	}
	return nil
}
