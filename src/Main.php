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

    private function v3(): array
    {
        $this->connect();

        $q = $this->pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r) {
            $r[(int) $code][$method][$error] = $error;
        });

        $hr = [];
        $q = $this->pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$hr) {
            $hr[$error] = $description;
        });

        return [$r, $hr];
    }

    private function v4(): array
    {
        $this->connect();

        $q = $this->pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r) {
            $code = (int) $code;
            $error = \preg_replace('/_X(["_])?/', '_%d\1', $error);
            if (!\in_array($method, $r[$code][$error] ?? [])) {
                $r[$code][$error][] = $method;
            }
        });
        $hr = [];
        $q = $this->pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$hr) {
            $error = \preg_replace('/_X(["_])?/', '_%d\1', $error);
            $description = \str_replace(' X ', ' %d ', $description);
            $hr[$error] = $description;
        });

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

        $q = $this->pdo->prepare('SELECT error FROM error_descriptions');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN);
        foreach ($r as $error) {
            if (\strpos($error, 'INPUT_METHOD_INVALID') !== false || \strpos($error, 'INPUT_CONSTRUCTOR_INVALID') !== false || \strpos($error, 'Received bad_msg_notification') === 0) {
                $q = $this->pdo->prepare('DELETE FROM errors WHERE error=?');
                $q->execute([$error]);
                $q = $this->pdo->prepare('DELETE FROM error_descriptions WHERE error=?');
                $q->execute([$error]);
                echo 'Delete '.$error."\n";
            }
        }
        $q = $this->pdo->prepare('SELECT error, method FROM errors');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP);
        foreach ($r as $error => $methods) {
            if (\strpos($error, 'INPUT_METHOD_INVALID') !== false || \strpos($error, 'INPUT_CONSTRUCTOR_INVALID') !== false || \strpos($error, 'Received bad_msg_notification') === 0) {
                $q = $this->pdo->prepare('DELETE FROM errors WHERE error=?');
                $q->execute([$error]);
                $q = $this->pdo->prepare('DELETE FROM error_descriptions WHERE error=?');
                $q->execute([$error]);
                echo 'Delete '.$error."\n";
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
        [$r, $hr] = $this->v3();
        \file_put_contents('data/v3.json', \json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
        [$r, $hr] = $this->v4();
        \file_put_contents('data/v4.json', \json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
        $bot = $this->bot();
        \file_put_contents('data/bot.json', \json_encode(['ok' => true, 'result' => $bot]));
    }

    public function run(): void
    {
        if (PHP_SAPI === 'cli') {
            $this->cli();
        } elseif (isset($_REQUEST['error'], $_REQUEST['code'], $_REQUEST['method'])
            && $_REQUEST['error'] !== ''
            && $_REQUEST['method'] !== ''
            && \is_numeric($_REQUEST['code'])
            && !(
                \in_array($_REQUEST['error'], ['PEER_FLOOD', 'INPUT_CONSTRUCTOR_INVALID_X', 'USER_DEACTIVATED_BAN', 'INPUT_METHOD_INVALID', 'INPUT_FETCH_ERROR', 'AUTH_KEY_UNREGISTERED', 'SESSION_REVOKED', 'USER_DEACTIVATED', 'RPC_CALL_FAIL', 'RPC_MCGET_FAIL', 'INTERDC_5_CALL_ERROR', 'INTERDC_4_CALL_ERROR', 'INTERDC_3_CALL_ERROR', 'INTERDC_2_CALL_ERROR', 'INTERDC_1_CALL_ERROR', 'INTERDC_5_CALL_RICH_ERROR', 'INTERDC_4_CALL_RICH_ERROR', 'INTERDC_3_CALL_RICH_ERROR', 'INTERDC_2_CALL_RICH_ERROR', 'INTERDC_1_CALL_RICH_ERROR'])
                || \str_contains($_REQUEST['error'], 'Received bad_msg_notification')
                || \str_contains($_REQUEST['error'], 'FLOOD_WAIT_')
                || \str_contains($_REQUEST['error'], 'EMAIL_UNCONFIRMED_')
                || \str_contains($_REQUEST['error'], '_MIGRATE_')
                || \preg_match('/FILE_PART_\d*_MISSING/', $_REQUEST['error'])
            )
         ) {
            $error = $_REQUEST['error'];
            $method = $_REQUEST['error'];
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
        } else {
            exit(self::error('API for reporting Telegram RPC errors. For localized errors see https://rpc.madelineproto.xyz, to report a new error use the `code`, `method` and `error` GET/POST parameters.'));
        }
    }
}
