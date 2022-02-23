<?php

return new PDO('mysql:unix_socket=/var/run/mysqld/mysqld.sock;dbname=rpc;charset=utf8mb4', 'user', 'pass');
