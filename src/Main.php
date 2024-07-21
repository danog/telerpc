<?php

use Amp\Http\HttpStatus;
use Amp\Http\Server\DefaultErrorHandler;
use Amp\Http\Server\Driver\Client;
use Amp\Http\Server\Driver\Http2Driver;
use Amp\Http\Server\Driver\HttpDriver;
use Amp\Http\Server\Driver\HttpDriverFactory;
use Amp\Http\Server\ErrorHandler;
use Amp\Http\Server\FormParser\Form;
use Amp\Http\Server\Request;
use Amp\Http\Server\RequestHandler;
use Amp\Http\Server\Response;
use Amp\Http\Server\SocketHttpServer;
use Amp\Log\ConsoleFormatter;
use Amp\Log\StreamHandler;
use Amp\Mysql\MysqlConnectionPool;
use danog\MadelineProto\RPCErrorException;
use Monolog\Logger;
use Monolog\Processor\PsrLogMessageProcessor;

use function Amp\ByteStream\getStdout;

final class Main implements RequestHandler
{
    private const GLOBAL_CODES = [
        'FLOOD_WAIT_%d'            => 420,
        '2FA_CONFIRM_WAIT_%d'      => 420,
        'SLOWMODE_WAIT_%d'         => 420,
        'TAKEOUT_INIT_DELAY_%d'    => 420,
        'FLOOD_PREMIUM_WAIT_%d'    => 420,
        'FLOOD_TEST_PHONE_WAIT_%d' => 420,

        'SESSION_PASSWORD_NEEDED' => 401,

        'AUTH_KEY_DUPLICATED' => 406,

        'FILE_MIGRATE_%d'    => 303,
        'NETWORK_MIGRATE_%d' => 303,
        'PHONE_MIGRATE_%d'   => 303,
        'STATS_MIGRATE_%d'   => 303,
        'USER_MIGRATE_%d'    => 303,

        'BOT_GAMES_DISABLED'                => 400,
        'BOT_METHOD_INVALID'                => 400,
        'BOT_POLLS_DISABLED'                => 400,
        'CONNECTION_DEVICE_MODEL_EMPTY'     => 400,
        'CONNECTION_LANG_PACK_INVALID'      => 400,
        'CONNECTION_NOT_INITED'             => 400,
        'CONNECTION_SYSTEM_EMPTY'           => 400,
        'CONNECTION_SYSTEM_LANG_CODE_EMPTY' => 400,
        'EMAIL_UNCONFIRMED_%d'              => 400,
        'FILE_MIGRATE_%d'                   => 400,
        'FILE_PART_%d_MISSING'              => 400,
        'INPUT_CONSTRUCTOR_INVALID'         => 400,
        'INPUT_FETCH_ERROR'                 => 400,
        'INPUT_FETCH_FAIL'                  => 400,
        'INPUT_LAYER_INVALID'               => 400,
        'INPUT_METHOD_INVALID'              => 400,
        'INPUT_REQUEST_TOO_LONG'            => 400,
        'PASSWORD_TOO_FRESH_%d'             => 400,
        'PEER_FLOOD'                        => 400,
        'PHOTO_THUMB_URL_INVALID'           => 400,
        'POLL_VOTE_REQUIRED'                => 400,
        'REPLY_MARKUP_GAME_EMPTY'           => 400,
        'SESSION_TOO_FRESH_%d'              => 400,
        'STICKERSET_NOT_MODIFIED'           => 400,
        'TMP_PASSWORD_INVALID'              => 400,
        'WEBDOCUMENT_URL_EMPTY'             => 400,

        'ACTIVE_USER_REQUIRED'    => 401,
        'AUTH_KEY_INVALID'        => 401,
        'AUTH_KEY_PERM_EMPTY'     => 401,
        'AUTH_KEY_UNREGISTERED'   => 401,
        'SESSION_EXPIRED'         => 401,
        'SESSION_PASSWORD_NEEDED' => 401,
        'SESSION_REVOKED'         => 401,
        'USER_DEACTIVATED'        => 401,
        'USER_DEACTIVATED_BAN'    => 401,

        'CHAT_FORBIDDEN' => 403,

        'AUTH_KEY_DUPLICATED' => 406,

        'MSG_WAIT_TIMEOUT' => -503,
        'MSG_WAIT_FAILED'  => -500,
    ];

    private MysqlConnectionPool $pool;

    public function __construct()
    {
        $this->pool = new MysqlConnectionPool(include __DIR__.'/../db.php');
    }

    private const HEADERS = [
        'Content-Type'                  => 'application/json',
        'access-control-allow-origin'   => '*',
        'access-control-allow-methods'  => 'GET, POST, OPTIONS',
        'access-control-expose-headers' => 'Content-Length,Content-Type,Date,Server,Connection',
    ];

    private static function error(string $message, int $code = HttpStatus::BAD_REQUEST): Response
    {
        return new Response(
            $code,
            self::HEADERS,
            json_encode(['ok' => false, 'description' => $message])
        );
    }

    private static function ok(mixed $result): Response
    {
        return new Response(
            HttpStatus::OK,
            self::HEADERS,
            json_encode(['ok' => false, 'result' => $result])
        );
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
        $desc = [];
        $q = $this->pool->prepare('SELECT error, description FROM error_descriptions');
        foreach ($q->execute() as ['error' => $error, 'description' => $description]) {
            $desc[$error] = $description;
        }

        $q = $this->pool->prepare('SELECT method, code, error FROM errors WHERE code != 500');
        $r = [];
        foreach ($q->execute() as ['method' => $method, 'code' => $code, 'error' => $error]) {
            $code = (int) $code;
            $r[$method][] = [
                'error_code'        => $code,
                'error_message'     => $error,
                'error_description' => $desc[$error] ?? '',
            ];
        }

        return $r;
    }

    private function v3(): array
    {
        $q = $this->pool->prepare('SELECT method, code, error FROM errors');
        $r = [];
        foreach ($q->execute() as ['method' => $method, 'code' => $code, 'error' => $error]) {
            $error = self::sanitize($error);
            $r[(int) $code][$method][$error] = $error;
        }

        $hr = [];
        $q = $this->pool->prepare('SELECT error, description FROM error_descriptions');
        foreach ($q->execute() as ['error' => $error, 'description' => $description]) {
            $error = self::sanitize($error);
            $description = \str_replace(' X ', ' %d ', $description);
            $hr[$error] = $description;
        }

        return [$r, $hr];
    }

    private function v4(bool $core = false): array
    {
        $q = $this->pool->prepare('SELECT method, code, error FROM errors');
        $r = [];
        $errors = [];
        $bot_only = [];
        foreach ($q->execute() as ['method' => $method, 'code' => $code, 'error' => $error]) {
            $code = (int) $code;
            $error = self::sanitize($error);
            if (!\in_array($method, $r[$code][$error] ?? [])) {
                $r[$code][$error][] = $method;
            }
            $errors[$error] = true;
            if (\in_array($error, ['USER_BOT_REQUIRED', 'USER_BOT_INVALID']) && !\in_array($method, $bot_only) && !\in_array($method, ['bots.setBotInfo', 'bots.getBotInfo'])) {
                $bot_only[] = $method;
            }
        }
        $hr = [];
        $q = $this->pool->prepare('SELECT error, description FROM error_descriptions');
        foreach ($q->execute() as ['error' => $error, 'description' => $description]) {
            $error = self::sanitize($error);
            $description = \str_replace(' X ', ' %d ', $description);
            if ($description !== '' && !\in_array($description[\strlen($description) - 1], ['?', '.', '!'])) {
                $description .= '.';
            }
            $hr[$error] = $description;
        }

        $hr['FLOOD_WAIT_%d'] = 'Please wait %d seconds before repeating the action.';

        foreach ($hr as $err => $_) {
            if (isset($errors[$err])) {
                continue;
            }
            if (!isset(self::GLOBAL_CODES[$err])) {
                echo "Missing code for $err!\n";
                continue;
            }
            $r[self::GLOBAL_CODES[$err]][$err] = [];
        }

        return [$r, $hr, $bot_only];
    }

    private function bot(): array
    {
        $q = $this->pool->prepare('SELECT method FROM bot_method_invalid');
        $r = [];
        foreach ($q->execute() as $result) {
            $r[] = $result['method'];
        }

        return $r;
    }

    private function cli(): void
    {
        $bot_only = [];
        $q = $this->pool->prepare('SELECT error, method FROM errors');
        $r = [];
        foreach ($q->execute() as $result) {
            $r[$result['error']][] = $result['method'];
        }
        foreach ($r as $error => $methods) {
            $fixed = self::sanitize($error);
            if ($fixed !== $error) {
                foreach ($methods as $method) {
                    $q = $this->pool->prepare('UPDATE errors SET error=? WHERE error=? AND method=?');
                    $q->execute([$fixed, $error, $method]);

                    $q = $this->pool->prepare('UPDATE error_descriptions SET error=? WHERE error=?');
                    $q->execute([$fixed, $error]);

                    if (self::sanitize($method) === $fixed) {
                        $q = $this->pool->prepare('DELETE FROM errors WHERE error=? AND method=?');
                        $q->execute([$fixed, $fixed]);
                        echo 'Delete strange '.$error."\n";
                    }
                }
                echo "$error => $fixed\n";
            }
            foreach ($methods as $method) {
                if (self::sanitize($method) === $fixed) {
                    $q = $this->pool->prepare('DELETE FROM errors WHERE error=? AND method=?');
                    $q->execute([$fixed, $method]);
                    echo 'Delete strange '.$error."\n";
                }
            }
        }

        $allowed = [];

        $q = $this->pool->prepare('SELECT error, method FROM errors');
        $r = [];
        foreach ($q->execute() as $result) {
            $r[$result['error']][] = $result['method'];
        }
        foreach (\array_merge($r, self::GLOBAL_CODES) as $error => $methods) {
            if (\is_int($methods)) {
                $allowed[$error] = true;
                $methods = [];
            }
            $anyok = false;
            foreach ($methods as $method) {
                if (RPCErrorException::isBad($error, 0, $method)) {
                    $q = $this->pool->prepare('DELETE FROM errors WHERE error=? AND method=?');
                    $q->execute([$error, $method]);
                    echo "Delete $error for $method\n";
                    continue;
                }
                $anyok = true;
            }
            if (!$anyok && $methods) {
                continue;
            }
            $allowed[$error] = true;

            $q = $this->pool->prepare('SELECT description FROM error_descriptions WHERE error=?');
            $res = $q->execute([$error])->fetchRow();
            if (!($res['description'] ?? null)) {
                $methods = \implode(', ', $methods);
                $description = \readline('Insert description for '.$error.' ('.$methods.'): ');
                if (\strpos(\readline($error.' - '.$description.' OK? '), 'n') !== false || !$description) {
                    continue;
                }
                if ($description === 'drop' || $description === 'delete' || $description === 'd') {
                    $q = $this->pool->prepare('DELETE FROM errors WHERE error=?');
                    $q->execute([$error]);
                    echo 'Delete '.$error."\n";
                } else {
                    $q = $this->pool->prepare('REPLACE INTO error_descriptions VALUES (?, ?)');
                    $q->execute([$error, $description]);
                }
            }
        }

        $q = $this->pool->prepare('SELECT error FROM error_descriptions');
        foreach ($q->execute() as ['error' => $error]) {
            if (!isset($allowed[$error])) {
                $q = $this->pool->prepare('DELETE FROM errors WHERE error=?');
                $q->execute([$error]);
                $q = $this->pool->prepare('DELETE FROM error_descriptions WHERE error=?');
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

        [$r, $hr, $bot_only] = $this->v4(true);
        \file_put_contents('data/core.json', \json_encode(['errors' => $r, 'descriptions' => $hr, 'user_only' => $bot, 'bot_only' => $bot_only]));
    }

    public function handleRequest(Request $request): Response
    {
        $form = Form::fromRequest($request);
        $error = $request->getQueryParameter('error') ?? $form->getValue('error');
        $code = $request->getQueryParameter('code') ?? $form->getValue('code');
        $method = $request->getQueryParameter('method') ?? $form->getValue('method');
        if ($error && $code && $method
            && \is_numeric($code)
            && !RPCErrorException::isBad(
                $error,
                (int) $code,
                $method
            )
        ) {
            $error = self::sanitize($error);

            try {
                if ($error === $code) {
                    $this->pool->prepare('REPLACE INTO code_errors VALUES (?);')->execute([$code]);
                } elseif ($error === 'BOT_METHOD_INVALID') {
                    $this->pool->prepare('REPLACE INTO bot_method_invalid VALUES (?);')->execute([$method]);
                } else {
                    $this->pool->prepare('REPLACE INTO errors VALUES (?, ?, ?);')->execute([$error, $method, $code]);

                    $q = $this->pool->prepare('SELECT description FROM error_descriptions WHERE error=?');
                    $result = $q->execute([$error]);
                    if ($row = $result->fetchRow()) {
                        return self::ok($row['description']);
                    }

                    return self::error('No description', 404);
                }
            } catch (\Throwable $e) {
                return self::error($e->getMessage(), 500);
            }

            return self::ok(true);
        }

        return self::error('API for reporting Telegram RPC errors. For localized errors see https://rpc.madelineproto.xyz, to report a new error use the `code`, `method` and `error` GET/POST parameters. Source code at https://github.com/danog/telerpc.');
    }

    public function run(string $mode): void
    {
        if ($mode !== 'serve' && $mode !== 'serve_h2c') {
            $this->cli();
            exit;
        }

        $logHandler = new StreamHandler(getStdout());
        $logHandler->pushProcessor(new PsrLogMessageProcessor());
        $logHandler->setFormatter(new ConsoleFormatter());

        $logger = new Logger('server');
        $logger->pushHandler($logHandler);
        $errorHandler = new DefaultErrorHandler();

        $server = SocketHttpServer::createForDirectAccess(
            $logger,
            true,
            PHP_INT_MAX,
            PHP_INT_MAX,
            PHP_INT_MAX,
            $mode === 'serve'
                ? null
                : new class($logger) implements HttpDriverFactory {
                    public function __construct(
                        private readonly Logger $logger,
                    ) {
                    }

                    public function createHttpDriver(
                        RequestHandler $requestHandler,
                        ErrorHandler $errorHandler,
                        Client $client,
                    ): HttpDriver {
                        return new Http2Driver(
                            requestHandler: $requestHandler,
                            errorHandler: $errorHandler,
                            logger: $this->logger,
                            concurrentStreamLimit: PHP_INT_MAX
                        );
                    }

                    public function getApplicationLayerProtocols(): array
                    {
                        return ['h2'];
                    }
                }
        );
        $server->expose('0.0.0.0:1337');
        $server->start($this, $errorHandler);

        // Serve requests until SIGINT or SIGTERM is received by the process.
        Amp\trapSignal([SIGINT, SIGTERM]);

        $server->stop();
    }
}
