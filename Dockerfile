FROM danog/madelineproto:latest

WORKDIR /app
ADD src /app
ADD composer.json /app
ADD server.php /app
ADD db.php /app
ADD entrypoint.sh /

RUN php -r "copy('https://getcomposer.org/installer', 'composer-setup.php');" && \
    php composer-setup.php && \
    php -r "unlink('composer-setup.php');" && \
    mv composer.phar /usr/local/bin/composer && \
    \
    apk add procps git unzip github-cli openssh && \
    composer update

ENTRYPOINT ["/entrypoint.sh"]

