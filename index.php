<?php

ini_set('log_errors', 1);
ini_set('error_log', '/tmp/rpc.log');
header('Content-Type: application/json');
function plsdie($message)
{
    die(json_encode(['ok' => false, 'description' => $message]));
}
if (php_sapi_name() === 'cli') {
    include 'db.php';
    $pdo->setAttribute(PDO::ATTR_EMULATE_PREPARES, false);
    $pdo->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);
    $q = $pdo->prepare('SELECT error FROM error_descriptions');
    $q->execute();
    $r = $q->fetchAll(PDO::FETCH_COLUMN);
    foreach ($r as $error) {
        if (strpos($error, 'INPUT_METHOD_INVALID') !== false || strpos($error, 'Received bad_msg_notification') === 0) {
            $q = $pdo->prepare('DELETE FROM errors WHERE error=?');
            $q->execute([$error]);
            $q = $pdo->prepare('DELETE FROM error_descriptions WHERE error=?');
            $q->execute([$error]);
            echo 'Delete '.$error."\n";
        }
    }
    $q = $pdo->prepare('SELECT error, method FROM errors');
    $q->execute();
    $r = $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP);
    foreach ($r as $error => $methods) {
        if (strpos($error, 'INPUT_METHOD_INVALID') !== false || strpos($error, 'Received bad_msg_notification') === 0) {
            $q = $pdo->prepare('DELETE FROM errors WHERE error=?');
            $q->execute([$error]);
            $q = $pdo->prepare('DELETE FROM error_descriptions WHERE error=?');
            $q->execute([$error]);
            echo 'Delete '.$error."\n";
            continue;
        }
        $q = $pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
        $q->execute([$error]);
        if (!$q->rowCount() || !($er = $q->fetchColumn())) {
            $methods = implode(', ', $methods);
            $description = readline('Insert description for '.$error.' ('.$methods.'): ');
            if (strpos(readline($error.' - '.$description.' OK? '), 'n') !== false) {
                continue;
            }
            if ($description === 'drop') {
                $q = $pdo->prepare('DELETE FROM errors WHERE error=?');
                $q->execute([$error]);
                echo 'Delete '.$error."\n";
            } else {
                $q = $pdo->prepare('REPLACE INTO error_descriptions VALUES (?, ?)');
                $q->execute([$error, $description]);
            }
        }
    }
    $map = ['all' => 'v1', 'newall' => 'v2', 'allv3' => 'v3', 'allv4' => 'v4', 'bot' => 'bot'];
    foreach ($map as $query => $name) {
        $data = file_get_contents("https://rpc.pwrtelegram.xyz/?$query");
        $data = json_encode(json_decode($data, true), JSON_PRETTY_PRINT);
        file_put_contents("data/$name.json", $data);
    }
    die;
}

if (!isset($_REQUEST['allv4']) && !isset($_REQUEST['all']) && !isset($_REQUEST['newall']) && !isset($_REQUEST['format']) && !isset($_REQUEST['allv3']) && !isset($_REQUEST['for']) && !isset($_REQUEST['rip']) && !isset($_REQUEST['code_for']) && !isset($_REQUEST['description_for']) && !isset($_REQUEST['bot']) && !isset($_REQUEST['period']) && !isset($_REQUEST['floods'])) {
    if (!isset($_REQUEST['method']) || !isset($_REQUEST['error']) || !isset($_REQUEST['code']) || $_REQUEST['error'] === '' || $_REQUEST['method'] === '' || !is_numeric($_REQUEST['code'])) {
        plsdie('API for reporting Telegram RPC errors. For localized errors see https://rpc.madelineproto.xyz.');
    }
    if (in_array($_REQUEST['error'], ['INPUT_CONSTRUCTOR_INVALID_X', 'USER_DEACTIVATED_BAN', 'INPUT_METHOD_INVALID', 'INPUT_FETCH_ERROR', 'AUTH_KEY_UNREGISTERED', 'SESSION_REVOKED', 'USER_DEACTIVATED']) || strpos($_REQUEST['error'], 'FLOOD_WAIT_') !== false || strpos($_REQUEST['error'], 'EMAIL_UNCONFIRMED_') !== false || strpos($_REQUEST['error'], '_MIGRATE_') !== false || $_REQUEST['error'] === 'PEER_FLOOD' || preg_match('/FILE_PART_\d*_MISSING/', $_REQUEST['error'])) {
        plsdie('nop');
    }
}

try {
    include 'db.php';
    $pdo->setAttribute(PDO::ATTR_EMULATE_PREPARES, false);
    $pdo->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);

    if (isset($_REQUEST['period']) && isset($_REQUEST['method']) && isset($_REQUEST['number'])) {
        if (is_numeric($_REQUEST['method'])) {
            require 'vendor/autoload.php';
            $m = \danog\MadelineProto\Serialization::deserialize('tl.madeline');
            $method = $m->API->methods->find_by_id($m->pack_signed_int($_REQUEST['method']))['method'];
            if ($method === false) {
                plsdie('Could not find method by provided ID');
            }
        } else {
            $method = $_REQUEST['method'];
        }
        $number = $_REQUEST['number'];
        $period = $_REQUEST['period'];
        if (!isset($_REQUEST['additional_key'])) {
            $_REQUEST['additional_key'] = 'default';
        }
        if (!isset($_REQUEST['additional_value'])) {
            $_REQUEST['additional_value'] = 'default';
        }
        $res = $pdo->prepare('REPLACE INTO flood_wait (method, number, period, additional_key, additional_value) VALUES (?, ?, ?, ?, ?);')->execute([$method, $number, $period, $_REQUEST['additional_key'], $_REQUEST['additional_value']]);
        die(json_encode(['ok' => true]));
    }
    if (isset($_REQUEST['for'])) {
        if (is_numeric($_REQUEST['for'])) {
            require 'vendor/autoload.php';
            $m = \danog\MadelineProto\Serialization::deserialize('tl.madeline');
            $method = $m->API->methods->find_by_id($m->pack_signed_int($_REQUEST['for']))['method'];
            if ($method === false) {
                plsdie('Could not find method by provided ID');
            }
        } else {
            $method = $_REQUEST['for'];
        }
        $q = $pdo->prepare('SELECT code, error FROM errors WHERE method=?');
        $q->execute([$method]);
        $r = ['ok' => true, 'result' => $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP)];
        foreach ($r['result'] as $code => $errors) {
            foreach ($errors as $error) {
                $q = $pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
                $q->execute([$error]);
                if ($q->rowCount()) {
                    $r['human_result'][$code][$error] = $q->fetchColumn();
                }
            }
        }
        die(json_encode($r));
    }
    if (isset($_REQUEST['floods'])) {
        if (is_numeric($_REQUEST['floods'])) {
            require 'vendor/autoload.php';
            $m = \danog\MadelineProto\Serialization::deserialize('tl.madeline');
            $method = $m->API->methods->find_by_id($m->pack_signed_int($_REQUEST['floods']))['method'];
            if ($method === false) {
                plsdie('Could not find method by provided ID');
            }
        } else {
            $method = $_REQUEST['floods'];
        }
        $q = $pdo->prepare('SELECT period, number, additional_key, additional_value FROM flood_wait WHERE method=?');
        $q->execute([$method]);
        $r = ['ok' => true, 'result' => $q->fetchAll(PDO::FETCH_ASSOC)];
        die(json_encode($r));
    }
    if (isset($_REQUEST['rip'])) {
        $q = $pdo->prepare('SELECT COUNT(time) FROM rip WHERE time > FROM_UNIXTIME(?)');
        $q->execute([is_int($_REQUEST['rip']) ? $_REQUEST['rip'] : time() - 3600]);
        die(json_encode(['ok' => true, 'result' => $q->fetchColumn()]));
    }
    if (isset($_REQUEST['all'])) {
        $q = $pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r) {
            if ($code === 500) {
                return;
            }
            $r[(int) $code][$method][] = $error;
        });

        $q = $pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $hr = $q->fetchAll(PDO::FETCH_COLUMN | PDO::FETCH_GROUP);

        die(json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
    }
    if (isset($_REQUEST['newall'])) {
        $desc = [];
        $q = $pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$desc) {
            $desc[$error] = $description;
        });

        $q = $pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r, &$desc) {
            if ($code === 500) {
                return;
            }
            $r[$method][] = [
                'error_code'        => $code,
                'error_message'     => $error,
                'error_description' => $desc[$error],
            ];
        });

        die(json_encode(['ok' => true, 'result' => $r]));
    }
    if (isset($_REQUEST['allv3'])) {
        $q = $pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r) {
            $r[(int) $code][$method][$error] = $error;
        });

        $hr = [];
        $q = $pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$hr) {
            $hr[$error] = $description;
        });

        die(json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
    }
    if (isset($_REQUEST['allv4'])) {
        $q = $pdo->prepare('SELECT method, code, error FROM errors');
        $q->execute();
        $r = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($method, $code, $error) use (&$r) {
            $code = (int) $code;
            $error = preg_replace('/_X(["_])?/', '_%d\1', $error);
            if (in_array($method, $r[$code][$error])) {
                return;
            }
            $r[$code][$error][] = $method;
        });
        $hr = [];
        $q = $pdo->prepare('SELECT error, description FROM error_descriptions');
        $q->execute();
        $q->fetchAll(PDO::FETCH_FUNC, function ($error, $description) use (&$hr) {
            $error = preg_replace('/_X(["_])?/', '_%d\1', $error);
            $description = str_replace(' X ', ' %d ', $description);
            $hr[$error] = $description;
        });

        die(json_encode(['ok' => true, 'result' => $r, 'human_result' => $hr]));
    }
    if (isset($_REQUEST['format'])) {
        header('Content-Type: text/plain');
        $q = $pdo->prepare('SELECT code, error FROM errors where method=?');
        $q->execute([$_REQUEST['format']]);
        $r = "| Code | Type     | Description   |\n|------|----------|---------------|\n";
        $temp = [];
        $q->fetchAll(PDO::FETCH_FUNC, function ($code, $error) use (&$temp) {
            if ($code === 500) {
                return;
            }
            if ($code === -503) {
                return;
            }
            $temp[$error] = $code;
        });
        foreach ($temp as $error => $code) {
            $q = $pdo->prepare('SELECT description FROM error_descriptions where error=?');
            $q->execute([$error]);
            $r .= "|$code|$error|".$q->fetchColumn()."|\n";
        }

        die($r);
    }
    if (isset($_REQUEST['bot'])) {
        $q = $pdo->prepare('SELECT method FROM bot_method_invalid');
        $q->execute();
        $r = $q->fetchAll(PDO::FETCH_COLUMN);
        die(json_encode(['ok' => true, 'result' => $r]));
    }
    if (isset($_REQUEST['description_for'])) {
        $q = $pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
        $q->execute([$_REQUEST['description_for']]);
        if ($q->rowCount()) {
            die(json_encode(['ok' => true, 'result' => $q->fetchColumn()]));
        } else {
            plsdie('No description');
        }
    }
    if (isset($_REQUEST['code_for'])) {
        $q = $pdo->prepare('SELECT code FROM errors WHERE error=?');
        $q->execute([$_REQUEST['code_for']]);
        if ($q->rowCount()) {
            die(json_encode(['ok' => true, 'result' => $q->fetchColumn()]));
        } else {
            plsdie('No such error');
        }
    }
    if ($_REQUEST['error'] === $_REQUEST['code']) {
        $res = $pdo->prepare('REPLACE INTO code_errors VALUES (?);')->execute([$_REQUEST['code']]);
    } elseif (in_array($_REQUEST['error'], ['RPC_CALL_FAIL', 'RPC_MCGET_FAIL', 'INTERDC_5_CALL_ERROR', 'INTERDC_4_CALL_ERROR', 'INTERDC_3_CALL_ERROR', 'INTERDC_2_CALL_ERROR', 'INTERDC_1_CALL_ERROR', 'INTERDC_5_CALL_RICH_ERROR', 'INTERDC_4_CALL_RICH_ERROR', 'INTERDC_3_CALL_RICH_ERROR', 'INTERDC_2_CALL_RICH_ERROR', 'INTERDC_1_CALL_RICH_ERROR'])) {
        $res = $pdo->prepare('INSERT INTO rip VALUES (FROM_UNIXTIME(?));')->execute([time()]);
    } elseif (in_array($_REQUEST['error'], ['BOT_METHOD_INVALID'])) {
        if (is_numeric($_REQUEST['method'])) {
            require 'vendor/autoload.php';
            $m = \danog\MadelineProto\Serialization::deserialize('tl.madeline');
            $method = $m->API->methods->find_by_id($m->pack_signed_int($_REQUEST['method']))['method'];
            if ($method === false) {
                plsdie('Could not find method by provided ID');
            }
        } else {
            $method = $_REQUEST['method'];
        }
        $res = $pdo->prepare('REPLACE INTO bot_method_invalid VALUES (?);')->execute([$method]);
    } else {
        if (is_numeric($_REQUEST['method'])) {
            require 'vendor/autoload.php';
            $m = \danog\MadelineProto\Serialization::deserialize('tl.madeline');
            $method = $m->API->methods->find_by_id($m->pack_signed_int($_REQUEST['method']))['method'];
            if ($method === false) {
                plsdie('Could not find method by provided ID');
            }
        } else {
            $method = $_REQUEST['method'];
        }
        $res = $pdo->prepare('REPLACE INTO errors VALUES (?, ?, ?);')->execute([$_REQUEST['error'], $method, $_REQUEST['code']]);

        $q = $pdo->prepare('SELECT description FROM error_descriptions WHERE error=?');
        $q->execute([$_REQUEST['error']]);
        if ($q->rowCount()) {
            die(json_encode(['ok' => true, 'result' => $q->fetchColumn()]));
        } else {
            plsdie('No description');
        }
    }
} catch (\PDOException $e) {
    plsdie($e->getMessage());
} catch (\danog\MadelineProto\Exception $e) {
    plsdie($e->getMessage());
} catch (\danog\MadelineProto\TL\Exception $e) {
    plsdie($e->getMessage());
}
echo json_encode(['ok' => $res]);
