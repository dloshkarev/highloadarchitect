package feed

// FeedKey возвращает ключ Redis Sorted Set с идентификаторами постов ленты.
//
//nolint:revive // имя FeedKey читается лучше, чем feed.Key на месте вызова.
func FeedKey(userID string) string {
	return "feed:" + userID
}

// PostKey возвращает ключ Redis с JSON-телом поста.
func PostKey(postID string) string {
	return "post:" + postID
}
