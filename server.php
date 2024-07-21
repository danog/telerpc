<?php

chdir(__DIR__);

require __DIR__.'/vendor/autoload.php';

(new Main())->run($argv[1] ?? '');
