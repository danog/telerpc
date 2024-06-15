<?php

use danog\MadelineProto\RPCErrorException;

final class Main
{
    private const GLOBAL_CODES = [
        'FLOOD_WAIT_%d'           => 420,
        '2FA_CONFIRM_WAIT_%d'     => 420,
        'SLOWMODE_WAIT_%d'        => 420,
        'TAKEOUT_INIT_DELAY_%d'   => 420,
        'FLOOD_PREMIUM_WAIT_%d'   => 420,
        'SESSION_PASSWORD_NEEDED' => 401,
    ];

    private ?\PDO $pdo = null;

    private function connect(): \PDO
    {
        if ($this->pdo !== null) {
            return $this->pdo;
        }
        $this->pdo = include __DIR__.'/../db.php';
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
        $errors = [];
        $bot_only = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r, &$bot_only, &$errors, $core) {
            if ($core && ($error === 'UPDATE_APP_TO_LOGIN' || $error === 'UPDATE_APP_REQUIRED')) {
                return;
            }
            $code = (int) $code;
            $error = self::sanitize($error);
            if (!\in_array($method, $r[$code][$error] ?? [])) {
                $r[$code][$error][] = $method;
            }
            $errors[$error] = true;
            if (\in_array($error, ['USER_BOT_REQUIRED', 'USER_BOT_INVALID']) && !\in_array($method, $bot_only) && !in_array($method, ['bots.setBotInfo', 'bots.getBotInfo'])) {
                $bot_only[] = $method;
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

        foreach ($hr as $err => $_) {
            if (isset($errors[$err])) {
                continue;
            }
            if (!isset(self::GLOBAL_CODES[$err])) {
                throw new AssertionError("Missing code for $err!");
            }
            $r[self::GLOBAL_CODES[$err]][$err] = [];
        }

        return [$r, $hr, $bot_only];
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

        $bot_only = [];
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

        $q = $this->pdo->prepare('SELECT error, method FROM errors');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP);
        foreach (array_merge($r, self::GLOBAL_CODES) as $error => $methods) {
            if (is_int($methods)) {
                $methods = [];
            }
            $anyok = false;
            foreach ($methods as $method) {
                if (RPCErrorException::isBad($error, 0, $method)) {
                    $q = $this->pdo->prepare('DELETE FROM errors WHERE error=? AND method=?');
                    $q->execute([$error, $method]);
                    echo "Delete $error for $method\n";
                    continue;
                }
                $anyok = true;
            }
            if (!$anyok) {
                continue;
            }

            $q = $this->pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
            $q->execute([$error]);
            if (!$q->rowCount() || !($er = $q->fetchColumn())) {
                $methods = \implode(', ', $methods);
                $description = \readline('Insert description for '.$error.' ('.$methods.'): ');
                if (\strpos(\readline($error.' - '.$description.' OK? '), 'n') !== false) {
                    continue;
                }
                if ($description === 'drop' || $description === 'delete' || $description === 'd') {
                    $q = $this->pdo->prepare('DELETE FROM errors WHERE error=?');
                    $q->execute([$error]);
                    echo 'Delete '.$error."\n";
                } else {
                    $q = $this->pdo->prepare('REPLACE INTO error_descriptions VALUES (?, ?)');
                    $q->execute([$error, $description]);
                }
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

        [$r, $hr, $bot_only] = $this->v4(true);
        \file_put_contents('data/core.json', \json_encode(['errors' => $r, 'descriptions' => $hr, 'user_only' => $bot, 'bot_only' => $bot_only]));
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
        \header('access-control-allow-origin: *');
        \header('access-control-allow-methods: GET, POST, OPTIONS');
        \header('access-control-expose-headers: Content-Length,Content-Type,Date,Server,Connection');
        if (isset($_REQUEST['error'], $_REQUEST['code'], $_REQUEST['method'])
            && $_REQUEST['error'] !== ''
            && $_REQUEST['method'] !== ''
            && \is_numeric($_REQUEST['code'])
            && !RPCErrorException::isBad(
                $_REQUEST['error'],
                (int) $_REQUEST['code'],
                $_REQUEST['method']
            )
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
