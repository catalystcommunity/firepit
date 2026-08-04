package csil

// This file keeps the operation-scoped codec names that the server tests
// used with the earlier csilgen release. The current generator emits shared
// type codecs when an operation uses a named group. Keep these small aliases
// outside generated files so regeneration does not remove the test client API.

func EncodeAuthBeginLoginRequest(v BeginLoginRequest) []byte { return EncodeBeginLoginRequest(v) }
func DecodeAuthBeginLoginResponse(v []byte) (BeginLoginResponse, error) {
	return DecodeBeginLoginResponse(v)
}
func EncodeAuthLogoutRequest(v Empty) []byte                         { return EncodeEmpty(v) }
func DecodeAuthLogoutResponse(v []byte) (Empty, error)               { return DecodeEmpty(v) }
func EncodeAuthWhoamiRequest(v Empty) []byte                         { return EncodeEmpty(v) }
func DecodeAuthWhoamiResponse(v []byte) (UserProfile, error)         { return DecodeUserProfile(v) }
func EncodeBoardCreateBoardRequest(v CreateBoardRequest) []byte      { return EncodeCreateBoardRequest(v) }
func DecodeBoardCreateBoardResponse(v []byte) (Board, error)         { return DecodeBoard(v) }
func EncodeEndorsementEndorseRequest(v EndorseRequest) []byte        { return EncodeEndorseRequest(v) }
func DecodeEndorsementEndorseResponse(v []byte) (Endorsement, error) { return DecodeEndorsement(v) }
func EncodeEndorsementListEndorsementsRequest(v TargetRef) []byte    { return EncodeTargetRef(v) }
func DecodeEndorsementListEndorsementsResponse(v []byte) (EndorsementList, error) {
	return DecodeEndorsementList(v)
}
func EncodeIntegrationCreateGithubMappingRequest(v CreateMappingRequest) []byte {
	return EncodeCreateMappingRequest(v)
}
func DecodeIntegrationCreateGithubMappingResponse(v []byte) (GithubMapping, error) {
	return DecodeGithubMapping(v)
}
func EncodeNotificationListNotificationsRequest(v ListNotificationsRequest) []byte {
	return EncodeListNotificationsRequest(v)
}
func DecodeNotificationListNotificationsResponse(v []byte) (NotificationPage, error) {
	return DecodeNotificationPage(v)
}
func EncodeReadMarkReadRequest(v TargetRef) []byte                    { return EncodeTargetRef(v) }
func DecodeReadMarkReadResponse(v []byte) (Empty, error)              { return DecodeEmpty(v) }
func EncodeReadMarkUnreadRequest(v TargetRef) []byte                  { return EncodeTargetRef(v) }
func DecodeReadMarkUnreadResponse(v []byte) (Empty, error)            { return DecodeEmpty(v) }
func EncodeReadUnreadSummaryRequest(v Empty) []byte                   { return EncodeEmpty(v) }
func DecodeReadUnreadSummaryResponse(v []byte) (UnreadSummary, error) { return DecodeUnreadSummary(v) }
func DecodeSocialResolveUserResponse(v []byte) (UserProfile, error)   { return DecodeUserProfile(v) }
func EncodeSubscriptionSubscribeRequest(v TargetRef) []byte           { return EncodeTargetRef(v) }
func DecodeSubscriptionSubscribeResponse(v []byte) (Subscription, error) {
	return DecodeSubscription(v)
}
func EncodeThreadCreateCommentRequest(v CreateCommentRequest) []byte {
	return EncodeCreateCommentRequest(v)
}
func DecodeThreadCreateCommentResponse(v []byte) (Comment, error)      { return DecodeComment(v) }
func EncodeThreadCreatePostRequest(v CreatePostRequest) []byte         { return EncodeCreatePostRequest(v) }
func DecodeThreadCreatePostResponse(v []byte) (Post, error)            { return DecodePost(v) }
func EncodeThreadEditCommentRequest(v EditCommentRequest) []byte       { return EncodeEditCommentRequest(v) }
func DecodeThreadEditCommentResponse(v []byte) (Comment, error)        { return DecodeComment(v) }
func EncodeThreadGetThreadRequest(v GetThreadRequest) []byte           { return EncodeGetThreadRequest(v) }
func DecodeThreadGetThreadResponse(v []byte) (Thread, error)           { return DecodeThread(v) }
func EncodeThreadListPostsRequest(v ListPostsRequest) []byte           { return EncodeListPostsRequest(v) }
func DecodeThreadListPostsResponse(v []byte) (PostPage, error)         { return DecodePostPage(v) }
func EncodeThreadListRevisionsRequest(v TargetRef) []byte              { return EncodeTargetRef(v) }
func DecodeThreadListRevisionsResponse(v []byte) (RevisionList, error) { return DecodeRevisionList(v) }
