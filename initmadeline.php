<?php

require_once 'vendor/autoload.php';
(new \danog\MadelineProto\API(['logger' => ['logger' => 1], 'tl_schema' => ['src' => ['telegram' => __DIR__.'/all.tl']], 'app_info' => ['api_id' => 6, 'api_hash' => 'eb06d4abfb49dc3eeb1aeb98ae0f581e']]))->serialize('tl.madeline');
