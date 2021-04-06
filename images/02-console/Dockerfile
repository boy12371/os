FROM sveil/os-base
COPY build/lsb-release /etc/
COPY build/sshd_config.append.tpl /etc/ssh/
COPY prompt.sh /etc/profile.d/
RUN sed -i 's/sveil:!/sveil:*/g' /etc/shadow && \
    sed -i 's/docker:!/docker:*/g' /etc/shadow && \
    echo '## allow password less for sveil user' >> /etc/sudoers && \
    echo 'sveil ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers && \
    echo '## allow password less for docker user' >> /etc/sudoers && \
    echo 'docker ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers && \
    cat /etc/ssh/sshd_config > /etc/ssh/sshd_config.tpl && \
    cat /etc/ssh/sshd_config.append.tpl >> /etc/ssh/sshd_config.tpl && \
    rm -f /etc/ssh/sshd_config.append.tpl /etc/ssh/sshd_config
