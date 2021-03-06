package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Necroforger/youtubearchive/models"
	"github.com/Necroforger/youtubearchive/util"

	"github.com/jinzhu/gorm"
)

const (
	paramQuery = "q"
	paramLimit = "limit"
	paramPage  = "p"
	paramID    = "id"
)

const (
	tplSearch   = "search"
	tplView     = "view"
	tplHome     = "home"
	tplChannels = "channels"
	tplLogin    = "login"
	tplProfile  = "profile"
)

func getSearchParams(r *http.Request) (query string, limit int, page int) {
	query = r.FormValue(paramQuery)

	limit = formValueInt(r, paramLimit, 0)
	page = formValueInt(r, paramPage, 0)
	return
}

func formValueInt(r *http.Request, key string, defaultValue int) int {
	v := r.FormValue(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

func queryTerminatedChannels(db *gorm.DB, query string, limit, page int) ([]models.Video, error) {
	var videos = []models.Video{}

	rows, err := db.Raw(
		"SELECT a.uploader, a.uploader_url FROM uploaders a INNER JOIN terminated_channels b ON a.uploader_url = b.uploader_url WHERE b.terminated=1 AND a.uploader LIKE ? LIMIT ? OFFSET ?",
		"%"+query+"%",
		limit,
		page*limit).
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		v := models.Video{}
		err := rows.Scan(&v.Uploader, &v.UploaderURL)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}

	return videos, err
}

func countTerminatedChannels(db *gorm.DB, query string) (int, error) {
	var count int
	err := db.Raw("SELECT count(*) FROM uploaders a INNER JOIN terminated_channels b ON a.uploader_url = b.uploader_url WHERE b.terminated=1 AND a.uploader LIKE ?", "%"+query+"%").Row().Scan(&count)

	return count, err
}

// queryChannel gives a list of channels
func queryChannels(db *gorm.DB, query string, limit, page int) ([]models.Video, error) {
	var videos = []models.Video{}

	rows, err := db.Raw(
		"SELECT uploader, uploader_url FROM uploaders WHERE uploader LIKE ? LIMIT ? OFFSET ?",
		"%"+query+"%",
		limit,
		page*limit).
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		v := models.Video{}
		err := rows.Scan(&v.Uploader, &v.UploaderURL)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}

	return videos, err
}

func countChannels(db *gorm.DB, query string) (int, error) {
	var count int
	err := db.Raw("SELECT count(*) FROM uploaders WHERE uploader LIKE ?", "%"+query+"%").Row().Scan(&count)

	return count, err
}

func queryVideos(db *gorm.DB, query string, limit int, page int) ([]models.Video, error) {
	var videos []models.Video

	raw, values := buildVideosQuery(query, limit, page)
	err := db.Raw(raw, values...).Scan(&videos).Error

	return videos, err
}

func countVideos(db *gorm.DB, query string, limit, page int) (int, error) {
	var count int
	raw, values := buildVideosQuery(query, limit, page)
	err := db.Raw("SELECT count(*) FROM ( "+raw+" )", values...).Row().Scan(&count)

	return count, err
}

func buildParts(field, prefix, value string) (raw string, values []interface{}) {
	parts := strings.Split(value, ",")
	for i := 0; i < len(parts); i++ {
		negate := func() string {
			if strings.HasPrefix(parts[i], "!") {
				parts[i] = strings.TrimPrefix(parts[i], "!")
				return " NOT"
			}
			return ""
		}
		if i == 0 {
			raw += prefix + " (" + field + negate() + " LIKE ?"
		} else {
			if strings.HasPrefix(parts[i], "||") {
				parts[i] = strings.TrimPrefix(parts[i], "||")
				raw += " OR " + field + " LIKE ?"
			} else {
				raw += " AND " + field + negate() + " LIKE ?"
			}
		}
		values = append(values, "%"+parts[i]+"%")
	}
	if len(parts) != 0 {
		raw += ")"
	}
	return
}

func buildVideosQuery(query string, limit, page int) (string, []interface{}) {
	exquery, tags, keys := util.ParseTags(query)

	raw := "SELECT a.* FROM videos a"

	// If we are querying for terminated videos, append a join statement to insert the terminated column into the results
	for _, v := range keys {
		if v == "terminated" {
			raw += " INNER JOIN terminated_channels b ON a.uploader_url = b.uploader_url"
			break
		}
	}

	values := []interface{}{}

	var exset bool
	if exquery != "" {
		raw += " WHERE title LIKE ?"
		values = append(values, "%"+exquery+"%")
		exset = true
	}

	for i, key := range keys {
		var prefix string
		if i == 0 {
			if !exset {
				prefix = " WHERE"
			} else {
				prefix = " AND"
			}
		} else {
			prefix = " AND"
		}

		addParts := func(r string, i []interface{}) {
			raw += r
			values = append(values, i...)
		}

		v := strings.ToLower(key)
		switch v {
		case "uploader":
			addParts(buildParts("uploader", prefix, tags[key]))
		case "description":
			addParts(buildParts("description", prefix, tags[key]))
		case "title":
			addParts(buildParts("title", prefix, tags[key]))
		case "terminated":
			addParts(buildParts("terminated", prefix, tags[key]))
		case "uploader_url":
			addParts(buildParts("uploader_url", prefix, tags[key]))
		}
	}

	raw += " ORDER BY upload_date DESC"
	if limit >= 0 {
		raw += " LIMIT ?"
		values = append(values, limit)
		if page >= 0 {
			raw += " OFFSET ?"
			values = append(values, page*limit)
		}
	}

	return raw, values
}

func getVideosByVideoID(db *gorm.DB, ID string, limit, page int) ([]models.Video, error) {
	var videos []models.Video

	err := db.Where("video_id = ?", ID).
		Limit(limit).
		Offset(page * limit).
		Order("last_scanned DESC").
		Find(&videos).
		Error

	return videos, err
}

// HandleSearch handles searches
func (s *Server) HandleSearch(w http.ResponseWriter, r *http.Request) {
	var (
		query, limit, page = getSearchParams(r)
	)
	if limit == 0 {
		limit = 100
	}

	var reterr error

	videos, err := queryVideos(s.DB, query, limit, page)
	if err != nil {
		s.Log("error querying videos: ", err)
		reterr = err
	}

	total, err := countVideos(s.DB, query, -1, -1)
	if err != nil {
		s.Log("error counting videos: ", err)
		reterr = err
	}
	total = int(float64(total)/float64(limit) + 0.9)

	s.ExecuteTemplate(w, r, tplSearch, map[string]interface{}{
		"pages":     []int{page - 1, page, page + 1},
		"query":     query,
		"title":     query,
		"limit":     limit,
		"videos":    videos,
		"err":       reterr,
		"paginator": NewPaginator(page, total, 31, query, limit, "/search"),
	})

}

// HandleView views a video
func (s *Server) HandleView(w http.ResponseWriter, r *http.Request) {
	var (
		id   = r.FormValue(paramID)
		page = formValueInt(r, paramPage, 0)
	)

	videos, err := getVideosByVideoID(s.DB, id, 100, page)

	s.ExecuteTemplate(w, r, tplView, map[string]interface{}{
		"videos": videos,
		"err":    err,
	})
}

// HandleHome ...
func (s *Server) HandleHome(w http.ResponseWriter, r *http.Request) {
	s.ExecuteTemplate(w, r, tplHome, map[string]interface{}{
		"title": "home",
	})
}

// HandleProfile fetches channel metadata information
func (s *Server) HandleProfile(w http.ResponseWriter, r *http.Request) {
	var (
		id = r.FormValue(paramID)
	)

	var profile map[string]interface{}
	var JSON string
	err := s.DB.Raw("select json from recent_channel_metadata where uploader_url=?", id).Row().Scan(&JSON)
	if err == nil {
		err = json.Unmarshal([]byte(JSON), &profile)
	} else {
		log.Println(err)
	}

	s.ExecuteTemplate(w, r, tplProfile, map[string]interface{}{
		"channel": profile,
		"err":     err,
	})
}

// HandleChannels ...
func (s *Server) HandleChannels(w http.ResponseWriter, r *http.Request) {
	var (
		query, limit, page = getSearchParams(r)
		terminated         = r.FormValue("terminated") == "1"
	)
	if limit == 0 {
		limit = 100
	}

	var reterror error

	var (
		videos []models.Video
		err    error
		total  int
	)
	if !terminated {
		videos, err = queryChannels(s.DB, query, limit, page)
		if err != nil {
			reterror = err
		}
		total, err = countChannels(s.DB, query)
		if err != nil {
			reterror = err
		}
	} else {
		videos, err = queryTerminatedChannels(s.DB, query, limit, page)
		if err != nil {
			reterror = err
		}

	}
	total = int(float64(total)/float64(limit) + 0.9)

	s.ExecuteTemplate(w, r, tplChannels, map[string]interface{}{
		"channels":  videos,
		"query":     query,
		"pages":     []int{page - 1, page, page + 1},
		"err":       reterror,
		"paginator": NewPaginator(page, total, 31, query, limit, "/channels"),
	})
}

// LoginHandler handles logins
func (s *Server) LoginHandler(w http.ResponseWriter, r *http.Request) {
	s.ExecuteTemplate(w, r, tplLogin, map[string]interface{}{
		"title":    "login",
		"redirect": r.FormValue("redirect"),
	})
}

// LoginHandlerPost ...
func (s *Server) LoginHandlerPost(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:  "password",
		Value: r.FormValue("password"),
	})
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/login"
	}
	http.Redirect(w, r, redir, http.StatusFound)
}

// LogoutHandler logs out
func (s *Server) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:    "password",
		Expires: time.Now().Add(time.Hour * -500),
	})
	http.Redirect(w, r, "/login?redirect="+url.QueryEscape(r.FormValue("redirect")), http.StatusFound)
}
