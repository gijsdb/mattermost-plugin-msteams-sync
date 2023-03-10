package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	sq "github.com/Masterminds/squirrel"
	pluginapi "github.com/mattermost/mattermost-plugin-api"
	"github.com/mattermost/mattermost-server/v6/plugin"
	"golang.org/x/oauth2"
)

const (
	avatarCacheTime = 300
	avatarKey       = "avatar_"
)

type ChannelLink struct {
	MattermostTeam    string
	MattermostChannel string
	MSTeamsTeam       string
	MSTeamsChannel    string
}

type Store interface {
	Init() error
	GetAvatarCache(userID string) ([]byte, error)
	SetAvatarCache(userID string, photo []byte) error
	GetLinkByChannelID(channelID string) (*ChannelLink, error)
	GetLinkByMSTeamsChannelID(teamID, channelID string) (*ChannelLink, error)
	DeleteLinkByChannelID(channelID string) error
	StoreChannelLink(link *ChannelLink) error
	TeamsToMattermostPostId(chatID string, postID string) (string, error)
	MattermostToTeamsPostId(postID string) (string, error)
	LinkPosts(mattermostPostID, chatOrChannelID, teamsPostID string) error
	GetTokenForMattermostUser(userID string) (*oauth2.Token, error)
	GetTokenForMSTeamsUser(userID string) (*oauth2.Token, error)
	SetUserInfo(userID string, msTeamsUserID string, token *oauth2.Token) error
	TeamsToMattermostUserId(userID string) (string, error)
	MattermostToTeamsUserId(userID string) (string, error)
	CheckEnabledTeamByTeamId(teamId string) bool
}

type StoreImpl struct {
	store        *pluginapi.StoreService
	api          plugin.API
	enabledTeams func() []string
	db           *sql.DB
}

func New(store *pluginapi.StoreService, api plugin.API, enabledTeams func() []string) *StoreImpl {
	return &StoreImpl{
		store:        store,
		api:          api,
		enabledTeams: enabledTeams,
	}
}

func (s *StoreImpl) Init() error {
	db, err := s.store.GetMasterDB()
	s.db = db
	if err != nil {
		return err
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS msteamssync_links (mmChannelID VARCHAR PRIMARY KEY, mmTeamID VARCHAR, msTeamsChannelID VARCHAR, msTeamsTeamID VARCHAR)")
	if err != nil {
		return err
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS msteamssync_users (mmUserID VARCHAR PRIMARY KEY, msTeamsUserID VARCHAR, token TEXT)")
	if err != nil {
		return err
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS msteamssync_posts (mmPostID VARCHAR PRIMARY KEY, msTeamsPostID VARCHAR, msTeamsChannelID VARCHAR)")
	if err != nil {
		return err
	}
	return nil
}

func (s *StoreImpl) GetAvatarCache(userID string) ([]byte, error) {
	data, appErr := s.api.KVGet(avatarKey + userID)
	if appErr != nil {
		return nil, appErr
	}
	return data, nil
}

func (s *StoreImpl) SetAvatarCache(userID string, photo []byte) error {
	appErr := s.api.KVSetWithExpiry(avatarKey+userID, photo, avatarCacheTime)
	if appErr != nil {
		return appErr
	}
	return nil

}

func (s *StoreImpl) GetLinkByChannelID(channelID string) (*ChannelLink, error) {
	query := s.getQueryBuilder().Select("mmChannelID, mmTeamID, msTeamsChannelID, msTeamsTeamID").From("msteamssync_links").Where(sq.Eq{"mmChannelID": channelID})
	row := query.QueryRow()
	var link ChannelLink
	err := row.Scan(&link.MattermostChannel, &link.MattermostTeam, &link.MSTeamsChannel, &link.MSTeamsTeam)
	if err != nil {
		return nil, err
	}

	if !s.CheckEnabledTeamByTeamId(link.MattermostTeam) {
		return nil, errors.New("link not enabled for this team")
	}
	return &link, nil
}

func (s *StoreImpl) GetLinkByMSTeamsChannelID(teamID, channelID string) (*ChannelLink, error) {
	query := s.getQueryBuilder().Select("mmChannelID, mmTeamID, msTeamsChannelID, msTeamsTeamID").From("msteamssync_links").Where(sq.Eq{"msTeamsChannelID": channelID})
	row := query.QueryRow()
	var link ChannelLink
	err := row.Scan(&link.MattermostChannel, &link.MattermostTeam, &link.MSTeamsChannel, &link.MSTeamsTeam)
	if err != nil {
		return nil, err
	}
	if !s.CheckEnabledTeamByTeamId(link.MattermostTeam) {
		return nil, errors.New("link not enabled for this team")
	}
	return &link, nil
}

func (s *StoreImpl) DeleteLinkByChannelID(channelID string) error {
	query := s.getQueryBuilder().Delete("msteamssync_links").Where(sq.Eq{"mmChannelID": channelID})
	_, err := query.Exec()
	if err != nil {
		return err
	}

	return nil
}

func (s *StoreImpl) StoreChannelLink(link *ChannelLink) error {
	query := s.getQueryBuilder().Insert("msteamssync_links").Columns("mmChannelID, mmTeamID, msTeamsChannelID, msTeamsTeamID").Values(link.MattermostChannel, link.MattermostTeam, link.MSTeamsChannel, link.MSTeamsTeam)
	_, err := query.Exec()
	if err != nil {
		return err
	}
	if !s.CheckEnabledTeamByTeamId(link.MattermostTeam) {
		return errors.New("link not enabled for this team")
	}
	return nil
}

func (s *StoreImpl) TeamsToMattermostUserId(userID string) (string, error) {
	query := s.getQueryBuilder().Select("mmUserID").From("msteamssync_users").Where(sq.Eq{"msTeamsUserID": userID})
	row := query.QueryRow()
	var mmUserID string
	err := row.Scan(&mmUserID)
	if err != nil {
		return "", err
	}
	return mmUserID, nil
}

func (s *StoreImpl) MattermostToTeamsUserId(userID string) (string, error) {
	query := s.getQueryBuilder().Select("msTeamsUserID").From("msteamssync_users").Where(sq.Eq{"mmUserID": userID})
	row := query.QueryRow()
	var msTeamsUserID string
	err := row.Scan(&msTeamsUserID)
	if err != nil {
		return "", err
	}
	return msTeamsUserID, nil
}

func (s *StoreImpl) TeamsToMattermostPostId(chatID string, postID string) (string, error) {
	query := s.getQueryBuilder().Select("mmPostID").From("msteamssync_posts").Where(sq.Eq{"msTeamsPostID": postID, "msTeamsChannelID": chatID})
	row := query.QueryRow()
	var mmPostID string
	err := row.Scan(&mmPostID)
	if err != nil {
		return "", err
	}
	return mmPostID, nil
}

func (s *StoreImpl) MattermostToTeamsPostId(postID string) (string, error) {
	query := s.getQueryBuilder().Select("msTeamsPostID").From("msteamssync_posts").Where(sq.Eq{"mmPostID": postID})
	row := query.QueryRow()
	var msTeamsPostID string
	err := row.Scan(&msTeamsPostID)
	if err != nil {
		return "", err
	}
	return msTeamsPostID, nil
}

func (s *StoreImpl) LinkPosts(mattermostPostID, teamsChatOrChannelID, teamsPostID string) error {
	query := s.getQueryBuilder().Insert("msteamssync_posts").Columns("mmPostID, msTeamsPostID, msTeamsChannelID").Values(mattermostPostID, teamsPostID, teamsChatOrChannelID)
	_, err := query.Exec()
	if err != nil {
		return err
	}
	return nil
}

func (s *StoreImpl) GetTokenForMattermostUser(userID string) (*oauth2.Token, error) {
	query := s.getQueryBuilder().Select("token").From("msteamssync_users").Where(sq.Eq{"mmUserID": userID})
	row := query.QueryRow()
	var tokendata string
	err := row.Scan(&tokendata)
	if err != nil {
		return nil, err
	}

	var token oauth2.Token
	err = json.Unmarshal([]byte(tokendata), &token)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *StoreImpl) GetTokenForMSTeamsUser(userID string) (*oauth2.Token, error) {
	query := s.getQueryBuilder().Select("token").From("msteamssync_users").Where(sq.Eq{"msTeamsUserID": userID})
	row := query.QueryRow()
	var tokendata string
	err := row.Scan(&tokendata)
	if err != nil {
		return nil, err
	}

	var token oauth2.Token
	err = json.Unmarshal([]byte(tokendata), &token)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *StoreImpl) SetUserInfo(userID string, msTeamsUserID string, token *oauth2.Token) error {
	tokendata := []byte{}
	if token != nil {
		var err error
		tokendata, err = json.Marshal(token)
		if err != nil {
			return err
		}
	}
	query := s.getQueryBuilder().Insert("msteamssync_users").Columns("mmUserID, msTeamsUserID, token").Values(userID, msTeamsUserID, string(tokendata)).Suffix("ON CONFLICT (mmUserID) DO UPDATE SET msTeamsUserID = EXCLUDED.msTeamsUserID, token = EXCLUDED.token")
	_, err := query.Exec()
	if err != nil {
		return err
	}
	return nil
}

func (s *StoreImpl) CheckEnabledTeamByTeamId(teamId string) bool {
	if len(s.enabledTeams()) == 1 && s.enabledTeams()[0] == "" {
		return true
	}
	team, appErr := s.api.GetTeam(teamId)
	if appErr != nil {
		return false
	}
	isTeamEnabled := false
	for _, enabledTeam := range s.enabledTeams() {
		if team.Name == enabledTeam {
			isTeamEnabled = true
			break
		}
	}
	return isTeamEnabled
}

func (s *StoreImpl) getQueryBuilder() sq.StatementBuilderType {
	builder := sq.StatementBuilder
	if s.store.DriverName() == "postgres" {
		builder = builder.PlaceholderFormat(sq.Dollar)
	}

	return builder.RunWith(s.db)
}
