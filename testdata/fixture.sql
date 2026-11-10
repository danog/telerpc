-- Rows for the export test, valid in both MariaDB and SQLite. testdata/export holds what the
-- PHP version (v2, v3, v4) wrote from them.
INSERT INTO settings (`key`, value) VALUES ('layer', 233);
INSERT INTO errors (error, method, code) VALUES
	('CHAT_ID_INVALID', 'messages.getChats', 400),
	('CHAT_ID_INVALID', 'messages.getFullChat', 400),
	('PEER_ID_INVALID', 'messages.getHistory', 400),
	('PEER_ID_INVALID', 'messages.sendMessage', 400),
	('INTERNAL_THING', 'messages.sendMessage', 500),
	('SLOWMODE_WAIT_%d', 'messages.sendMessage', 420),
	('FILE_REFERENCE_%d_EXPIRED', 'messages.sendMedia', 400),
	('AUTH_KEY_UNREGISTERED', 'messages.getHistory', 401),
	('USER_BOT_REQUIRED', 'bots.answerWebhookJSONQuery', 400),
	('USER_BOT_REQUIRED', 'bots.setBotInfo', 400),
	('USER_BOT_INVALID', 'bots.sendCustomRequest', 400),
	('BUSINESS_CONNECTION_INVALID', 'messages.sendMessage', 400),
	('BUSINESS_CONNECTION_INVALID', 'messages.sendMessage', 403),
	('BUSINESS_CONNECTION_INVALID', 'messages.editMessage', 400),
	('MESSAGE_EMPTY', 'test', 400),
	('Timeout', 'messages.getBotCallbackAnswer', -503);
INSERT INTO error_descriptions (error, description) VALUES
	('CHAT_ID_INVALID', 'The provided chat id is invalid.'),
	('PEER_ID_INVALID', 'The provided peer id is invalid'),
	('INTERNAL_THING', NULL),
	('SLOWMODE_WAIT_%d', 'Slowmode is enabled, wait X seconds'),
	('FILE_REFERENCE_%d_EXPIRED', 'File reference %d expired, see [file references](/api/file_reference)!'),
	('AUTH_KEY_UNREGISTERED', 'The specified authorization key is not registered in the system.'),
	('USER_BOT_REQUIRED', 'This method can only be called by a bot'),
	('BUSINESS_CONNECTION_INVALID', 'The `connection_id` is invalid / unknown?'),
	('MESSAGE_EMPTY', 'Café — the message is empty 😀'),
	('Timeout', 'Timeout while fetching data'),
	('SESSION_REVOKED', 'The session was revoked'),
	('ORPHAN_ERROR', 'Described, but never reported');
INSERT INTO bot_method_invalid (method) VALUES ('messages.getDialogs'), ('account.updateUsername'), ('contacts.getContacts');
INSERT INTO code_errors (code) VALUES (400);
