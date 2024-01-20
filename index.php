<?php

chdir(__DIR__);

require 'vendor/autoload.php';

include 'src/Main.php';

(new Main())->run();
