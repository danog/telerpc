FROM danog/madelineproto:latest

RUN php -r "copy('https://getcomposer.org/installer', 'composer-setup.php');" && \
    php composer-setup.php && \
    php -r "unlink('composer-setup.php');" && \
    mv composer.phar /usr/local/bin/composer && \
    \
    apk add procps git unzip github-cli openssh 

RUN git clone https://github.com/danog/telerpc /app

WORKDIR /app

ENTRYPOINT ["/app/entrypoint.sh"]

