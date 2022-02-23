<?php

\ini_set('log_errors', 1);
\ini_set('error_log', '/tmp/rpc.log');
\header('Content-Type: application/json');

include 'src/Main.php';

(new Main)->run();
