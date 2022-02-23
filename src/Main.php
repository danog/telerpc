<?php

final class Main
{
    private ?\PDO $pdo = null;

    private function connect(): \PDO
    {
        if ($this->pdo !== null) {
            return $this->pdo;
        }
        $this->pdo = include 'db.php';
        $this->pdo->setAttribute(PDO::ATTR_EMULATE_PREPARES, false);
        $this->pdo->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);

        return $this->pdo;
    }

    private static function error(string $message): string
    {
        return \json_encode(['ok' => false, 'description' => $message]);
    }

    private static function ok(mixed $result): string
    {
        return \json_encode(['ok' => true, 'result' => $result]);
    }

    private static function isBad(string $error, int $code): bool
    {
        return \in_array($error, ['PEER_FLOOD', 'INPUT_CONSTRUCTOR_INVALID_X', 'USER_DEACTIVATED_BAN', 'INPUT_METHOD_INVALID', 'INPUT_FETCH_ERROR', 'AUTH_KEY_UNREGISTERED', 'SESSION_REVOKED', 'USER_DEACTIVATED', 'RPC_CALL_FAIL', 'RPC_MCGET_FAIL', 'INTERDC_5_CALL_ERROR', 'INTERDC_4_CALL_ERROR', 'INTERDC_3_CALL_ERROR', 'INTERDC_2_CALL_ERROR', 'INTERDC_1_CALL_ERROR', 'INTERDC_5_CALL_RICH_ERROR', 'INTERDC_4_CALL_RICH_ERROR', 'INTERDC_3_CALL_RICH_ERROR', 'INTERDC_2_CALL_RICH_ERROR', 'INTERDC_1_CALL_RICH_ERROR', 'AUTH_KEY_DUPLICATED', 'CONNECTION_NOT_INITED', 'LOCATION_NOT_AVAILABLE', 'AUTH_KEY_INVALID', 'BOT_METHOD_INVALID', 'LANG_CODE_EMPTY', 'memory limit exit', 'memory limit(?)', 'INPUT_REQUEST_TOO_LONG', 'SESSION_PASSWORD_NEEDED', 'INPUT_FETCH_FAIL',
            'CONNECTION_SYSTEM_EMPTY',
            'CONNECTION_DEVICE_MODEL_EMPTY', 'AUTH_KEY_PERM_EMPTY', 'UNKNOWN_METHOD', 'ENCRYPTION_OCCUPY_FAILED', 'ENCRYPTION_OCCUPY_ADMIN_FAILED', 'CHAT_OCCUPY_USERNAME_FAILED', 'REG_ID_GENERATE_FAILED',
            'CONNECTION_LANG_PACK_INVALID', 'MSGID_DECREASE_RETRY', 'API_CALL_ERROR', 'STORAGE_CHECK_FAILED', 'INPUT_LAYER_INVALID', 'NEED_MEMBER_INVALID', 'NEED_CHAT_INVALID', 'HISTORY_GET_FAILED', 'CHP_CALL_FAIL', 'IMAGE_ENGINE_DOWN', 'MSG_RANGE_UNSYNC', 'PTS_CHANGE_EMPTY',
            'CONNECTION_SYSTEM_LANG_CODE_EMPTY', 'WORKER_BUSY_TOO_LONG_RETRY', 'WP_ID_GENERATE_FAILED', 'ARR_CAS_FAILED', 'CHANNEL_ADD_INVALID', 'CHANNEL_ADMINS_INVALID', 'CHAT_OCCUPY_LOC_FAILED', 'GROUPED_ID_OCCUPY_FAILED', 'GROUPED_ID_OCCUPY_FAULED', 'LOG_WRAP_FAIL', 'MEMBER_FETCH_FAILED', 'MEMBER_OCCUPY_PRIMARY_LOC_FAILED', 'MEMBER_FETCH_FAILED', 'MEMBER_NO_LOCATION', 'MEMBER_OCCUPY_USERNAME_FAILED', 'MT_SEND_QUEUE_TOO_LONG', 'POSTPONED_TIMEOUT', 'RPC_CONNECT_FAILED', 'SHORTNAME_OCCUPY_FAILED', 'STORE_INVALID_OBJECT_TYPE', 'STORE_INVALID_SCALAR_TYPE', 'TMSG_ADD_FAILED', 'UNKNOWN_ERROR', 'UPLOAD_NO_VOLUME', 'USER_NOT_AVAILABLE', 'VOLUME_LOC_NOT_FOUND', ])
                || \str_contains($error, 'Received bad_msg_notification')
                || \str_contains($error, 'FLOOD_WAIT_')
                || \str_contains($error, '_MIGRATE_')
                || \str_contains($error, 'INPUT_METHOD_INVALID')
                || \str_contains($error, 'INPUT_CONSTRUCTOR_INVALID')
                || \str_starts_with($error, 'Received bad_msg_notification')
                || \str_starts_with($error, 'No workers running')
                || \str_starts_with($error, 'All workers are busy. Active_queries ')
                || \preg_match('/FILE_PART_\d*_MISSING/', $error);
    }

    private static function sanitize(string $error): string
    {
        $error = \preg_replace('/_XMIN$/', '_%dMIN', $error);
        $error = \preg_replace('/_\d+MIN$/', '_%dMIN', $error);
        $error = \preg_replace('/_X(["_])?/', '_%d\1', $error);
        $error = \preg_replace('/_\d+(["_])?/', '_%d\1', $error);
        $error = \preg_replace('/_X$/', '_%d', $error);

        return \preg_replace('/_\d+$/', '_%d', $error);
    }

    private function v2(): array
    {
        $this->connect();
        $desc = [];
        $q = $this->pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$desc) {
            $desc[$error] = $description;
        });

        $q = $this->pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r, &$desc) {
            $code = (int) $code;
            if ($code === 500) {
                return;
            }
            $r[$method][] = [
                'error_code'        => $code,
                'error_message'     => $error,
                'error_description' => $desc[$error] ?? '',
            ];
        });

        return $r;
    }

    private function v3(): array
    {
        $this->connect();

        $q = $this->pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r) {
            $error = self::sanitize($error);
            $r[(int) $code][$method][$error] = $error;
        });

        $hr = [];
        $q = $this->pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$hr) {
            $error = self::sanitize($error);
            $description = \str_replace(' X ', ' %d ', $description);
            $hr[$error] = $description;
        });

        return [$r, $hr];
    }

    private function v4(bool $core = false): array
    {
        $this->connect();

        $q = $this->pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r, $core) {
            if ($core && ($error === 'UPDATE_APP_TO_LOGIN' || $error === 'UPDATE_APP_REQUIRED')) {
                return;
            }
            $code = (int) $code;
            $error = self::sanitize($error);
            if (!\in_array($method, $r[$code][$error] ?? [])) {
                $r[$code][$error][] = $method;
            }
        });
        $hr = [];
        $q = $this->pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$hr, $core) {
            if ($core && ($error === 'UPDATE_APP_TO_LOGIN' || $error === 'UPDATE_APP_REQUIRED')) {
                return;
            }
            $error = self::sanitize($error);
            $description = \str_replace(' X ', ' %d ', $description);
            if ($description !== '' && !\in_array($description[\strlen($description) - 1], ['?', '.', '!'])) {
                $description .= '.';
            }
            $hr[$error] = $description;
        });

        $hr['FLOOD_WAIT_%d'] = 'Please wait %d seconds before repeating the action.';

        return [$r, $hr];
    }

    private function bot(): array
    {
        $this->connect();

        $q = $this->pdo->prepare('SELECT method FROM bot_method_invalid');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN);

        return $r;
    }

    private function cli(): void
    {
        $this->connect();

        $q = $this->pdo->prepare('SELECT error, method FROM errors');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP);
        foreach ($r as $error => $methods) {
            $fixed = self::sanitize($error);
            if ($fixed !== $error) {
                foreach ($methods as $method) {
                    $q = $this->pdo->prepare('UPDATE errors SET error=? WHERE error=? AND method=?');
                    $q->execute([$fixed, $error, $method]);

                    $q = $this->pdo->prepare('UPDATE error_descriptions SET error=? WHERE error=?');
                    $q->execute([$fixed, $error]);

                    if (self::sanitize($method) === $fixed) {
                        $q = $this->pdo->prepare('DELETE FROM errors WHERE error=? AND method=?');
                        $q->execute([$fixed, $fixed]);
                        echo 'Delete strange '.$error."\n";
                    }
                }
                echo "$error => $fixed\n";
            }
            foreach ($methods as $method) {
                if (self::sanitize($method) === $fixed) {
                    $q = $this->pdo->prepare('DELETE FROM errors WHERE error=? AND method=?');
                    $q->execute([$fixed, $method]);
                    echo 'Delete strange '.$error."\n";
                }
            }
        }

        $allowed = ['SESSION_PASSWORD_NEEDED' => true];

        $q = $this->pdo->prepare('SELECT error, method FROM errors');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP);
        foreach ($r as $error => $methods) {
            if (self::isBad($error, 0)) {
                $q = $this->pdo->prepare('DELETE FROM errors WHERE error=?');
                $q->execute([$error]);
                $q = $this->pdo->prepare('DELETE FROM error_descriptions WHERE error=?');
                $q->execute([$error]);
                echo 'Delete '.$error."\n";
                continue;
            }
            $allowed[$error] = true;

            $q = $this->pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
            $q->execute([$error]);
            if (!$q->rowCount() || !($er = $q->fetchColumn())) {
                $methods = \implode(', ', $methods);
                $description = \readline('Insert description for '.$error.' ('.$methods.'): ');
                if (\strpos(\readline($error.' - '.$description.' OK? '), 'n') !== false) {
                    continue;
                }
                if ($description === 'drop') {
                    $q = $this->pdo->prepare('DELETE FROM errors WHERE error=?');
                    $q->execute([$error]);
                    echo 'Delete '.$error."\n";
                } else {
                    $q = $this->pdo->prepare('REPLACE INTO error_descriptions VALUES (?, ?)');
                    $q->execute([$error, $description]);
                }
            }
        }

        $q = $this->pdo->prepare('SELECT error FROM error_descriptions');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN);
        foreach ($r as $error) {
            if (!isset($allowed[$error])) {
                $q = $this->pdo->prepare('DELETE FROM errors WHERE error=?');
                $q->execute([$error]);
                $q = $this->pdo->prepare('DELETE FROM error_descriptions WHERE error=?');
                $q->execute([$error]);
                echo 'Delete '.$error."\n";
            }
        }

        $r = $this->v2();
        \file_put_contents('data/v2.json', \json_encode(['ok' => true, 'result' => $r]));
        [$r, $hr] = $this->v3();
        \file_put_contents('data/v3.json', \json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
        [$r, $hr] = $this->v4();
        \file_put_contents('data/v4.json', \json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
        \file_put_contents('data/vdiff.json', \json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr], JSON_PRETTY_PRINT));
        $bot = $this->bot();
        \file_put_contents('data/bot.json', \json_encode(['ok' => true, 'result' => $bot]));
        \file_put_contents('data/botdiff.json', \json_encode(['ok' => true, 'result' => $bot], JSON_PRETTY_PRINT));

        [$r, $hr] = $this->v4(true);
        \file_put_contents('data/core.json', \json_encode(['errors' => $r, 'descriptions' => $hr, 'user_only' => $bot]));
    }

    public function run(): void
    {
        if (PHP_SAPI === 'cli') {
            $this->cli();
            exit;
        }

        \ini_set('log_errors', 1);
        \ini_set('error_log', '/tmp/rpc.log');
        \header('Content-Type: application/json');
        if (isset($_REQUEST['error'], $_REQUEST['code'], $_REQUEST['method'])
            && $_REQUEST['error'] !== ''
            && $_REQUEST['method'] !== ''
            && \is_numeric($_REQUEST['code'])
            && !self::isBad($_REQUEST['error'], (int) $_REQUEST['code'])
            && !($_REQUEST['error'] === 'Timeout' && !\in_array(\strtolower($_REQUEST['method']), ['messages.getbotcallbackanswer', 'messages.getinlinebotresults']))
         ) {
            $error = self::sanitize($_REQUEST['error']);
            $method = $_REQUEST['method'];
            $code = $_REQUEST['code'];

            try {
                $this->connect();
                if ($error === $code) {
                    $this->pdo->prepare('REPLACE INTO code_errors VALUES (?);')->execute([$code]);
                } elseif ($error === 'BOT_METHOD_INVALID') {
                    $this->pdo->prepare('REPLACE INTO bot_method_invalid VALUES (?);')->execute([$method]);
                } else {
                    $this->pdo->prepare('REPLACE INTO errors VALUES (?, ?, ?);')->execute([$error, $method, $code]);

                    $q = $this->pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
                    $q->execute([$error]);
                    if ($q->rowCount()) {
                        exit(self::ok($q->fetchColumn()));
                    }
                    exit(self::error('No description'));
                }
            } catch (\Throwable $e) {
                exit(self::error($e->getMessage()));
            }
            exit(self::ok(true));
        }
        exit(self::error('API for reporting Telegram RPC errors. For localized errors see https://rpc.madelineproto.xyz, to report a new error use the `code`, `method` and `error` GET/POST parameters. Source code at https://github.com/danog/telerpc.'));
    }
}
