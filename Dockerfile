FROM debian:jessie

RUN groupadd -r hrotti && useradd -r -g hrotti hrotti

# Install Rails App
WORKDIR /app
ADD ./build/Linux/simple_broker /app/simple_broker

USER hrotti

CMD /app/simple_broker