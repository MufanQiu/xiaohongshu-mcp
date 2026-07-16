package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type UserProfileAction struct {
	page *rod.Page
}

func NewUserProfileAction(page *rod.Page) *UserProfileAction {
	pp := page.Timeout(60 * time.Second)
	return &UserProfileAction{page: pp}
}

// UserProfile 获取用户基本信息及帖子
func (u *UserProfileAction) UserProfile(ctx context.Context, userID, xsecToken string) (*UserProfileResponse, error) {
	page := u.page.Context(ctx)

	searchURL := makeUserProfileURL(userID, xsecToken)
	page.MustNavigate(searchURL)
	page.MustWaitStable()

	return u.extractUserProfileData(page)
}

// userPageData 用户主页的基本信息与互动数据
type userPageData struct {
	Interactions []UserInteractions `json:"interactions"`
	BasicInfo    UserBasicInfo      `json:"basicInfo"`
}

// extractUserPageData 提取 __INITIAL_STATE__.user.userPageData(基本信息与互动数据)
func extractUserPageData(page *rod.Page) (*userPageData, error) {
	page.MustWait(`() => window.__INITIAL_STATE__ !== undefined`)

	userDataResult := page.MustEval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.user &&
		    window.__INITIAL_STATE__.user.userPageData) {
			const userPageData = window.__INITIAL_STATE__.user.userPageData;
			const data = userPageData.value !== undefined ? userPageData.value : userPageData._value;
			if (data) {
				return JSON.stringify(data);
			}
		}
		return "";
	}`).String()

	if userDataResult == "" {
		return nil, fmt.Errorf("user.userPageData.value not found in __INITIAL_STATE__")
	}

	var data userPageData
	if err := json.Unmarshal([]byte(userDataResult), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal userPageData: %w", err)
	}
	return &data, nil
}

// extractUserProfileData 从页面中提取用户资料数据的通用方法
func (u *UserProfileAction) extractUserProfileData(page *rod.Page) (*UserProfileResponse, error) {
	pageData, err := extractUserPageData(page)
	if err != nil {
		return nil, err
	}

	// 2. 获取用户帖子：window.__INITIAL_STATE__.user.notes.value
	notesResult := page.MustEval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.user &&
		    window.__INITIAL_STATE__.user.notes) {
			const notes = window.__INITIAL_STATE__.user.notes;
			// 优先使用 value（getter），如果不存在则使用 _value（内部字段）
			const data = notes.value !== undefined ? notes.value : notes._value;
			if (data) {
				return JSON.stringify(data);
			}
		}
		return "";
	}`).String()

	if notesResult == "" {
		return nil, fmt.Errorf("user.notes.value not found in __INITIAL_STATE__")
	}

	// 解析帖子数据（帖子为双重数组）
	var notesFeeds [][]Feed
	if err := json.Unmarshal([]byte(notesResult), &notesFeeds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notes: %w", err)
	}

	// 组装响应
	response := &UserProfileResponse{
		UserBasicInfo: pageData.BasicInfo,
		Interactions:  pageData.Interactions,
	}

	// 添加用户帖子（展平双重数组）
	for _, feeds := range notesFeeds {
		if len(feeds) != 0 {
			response.Feeds = append(response.Feeds, feeds...)
		}
	}

	return response, nil
}

func makeUserProfileURL(userID, xsecToken string) string {
	return fmt.Sprintf("%s/user/profile/%s?xsec_token=%s&xsec_source=pc_note", Site().Base, userID, xsecToken)
}

func (u *UserProfileAction) GetMyProfileViaSidebar(ctx context.Context) (*UserProfileResponse, error) {
	page := u.page.Context(ctx)

	// 创建导航动作
	navigate := NewNavigate(page)

	// 通过侧边栏导航到个人主页
	if err := navigate.ToProfilePage(ctx); err != nil {
		return nil, fmt.Errorf("failed to navigate to profile page via sidebar: %w", err)
	}

	// 等待页面加载完成并获取 __INITIAL_STATE__
	page.MustWaitStable()

	return u.extractUserProfileData(page)
}

// 自己主页的内容 sub-tab 下标。点击后数据灌入 __INITIAL_STATE__.user.notes 对应下标。
const (
	MyTabSaved = 1 // 收藏
	MyTabLiked = 2 // 点赞
)

// GetMyTabFeeds 获取自己主页指定 sub-tab(收藏/点赞)的笔记列表。
// 经侧边栏进入自己主页,按下标点击 sub-tab(不依赖 tab 文本,兼容英文界面账号),
// 等待加载完成后从 __INITIAL_STATE__.user.notes[tab] 提取当前页数据。
func (u *UserProfileAction) GetMyTabFeeds(ctx context.Context, tab int) (*UserProfileResponse, error) {
	page := u.page.Context(ctx)

	navigate := NewNavigate(page)
	if err := navigate.ToProfilePage(ctx); err != nil {
		return nil, fmt.Errorf("failed to navigate to profile page via sidebar: %w", err)
	}
	page.MustWaitStable()

	// 按下标点击 sub-tab(0=笔记 1=收藏 2=点赞)
	tabs, err := page.Elements(`.reds-tab-item.sub-tab-list`)
	if err != nil {
		return nil, fmt.Errorf("failed to find profile sub-tabs: %w", err)
	}
	if len(tabs) <= tab {
		return nil, fmt.Errorf("profile sub-tab %d not found, only %d tabs", tab, len(tabs))
	}
	if err := tabs[tab].Click(proto.InputMouseButtonLeft, 1); err != nil {
		return nil, fmt.Errorf("failed to click profile sub-tab %d: %w", tab, err)
	}

	// 等待该 tab 数据拉取完成(isFetchingNotes[tab] 变回 false)
	page.MustWait(fmt.Sprintf(`() => {
		const u = window.__INITIAL_STATE__ && window.__INITIAL_STATE__.user;
		if (!u || !u.isFetchingNotes) return false;
		let fetching = u.isFetchingNotes;
		if (fetching.value !== undefined) fetching = fetching.value;
		else if (fetching._value !== undefined) fetching = fetching._value;
		return fetching[%d] === false;
	}`, tab))
	time.Sleep(500 * time.Millisecond)

	pageData, err := extractUserPageData(page)
	if err != nil {
		return nil, err
	}

	feedsResult := page.MustEval(fmt.Sprintf(`() => {
		const u = window.__INITIAL_STATE__.user;
		let notes = u.notes;
		if (notes.value !== undefined) notes = notes.value;
		else if (notes._value !== undefined) notes = notes._value;
		return JSON.stringify(notes[%d] || []);
	}`, tab)).String()

	var feeds []Feed
	if err := json.Unmarshal([]byte(feedsResult), &feeds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tab %d feeds: %w", tab, err)
	}

	return &UserProfileResponse{
		UserBasicInfo: pageData.BasicInfo,
		Interactions:  pageData.Interactions,
		Feeds:         feeds,
	}, nil
}
