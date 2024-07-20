<?php

use Amp\Mysql\MysqlConfig;

return new MysqlConfig(
    host: '/var/run/mysqld/mysqld.sock',
    user: 'user',
    password: 'pass',
    database: 'rpc',
);
