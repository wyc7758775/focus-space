-- The same private owner/profile mapping used for one-off Chat links also
-- scopes user-authored notes. Keep the source hidden from subscription
-- navigation while presenting the broader material semantics in Focus.
UPDATE feed_sources
SET name='对话资料',
    description='用户明确加入焦点的一次性公开文章或手工记录',
    updated_at=CURRENT_TIMESTAMP
WHERE id='focus-chat-links';
