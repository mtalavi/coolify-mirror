FROM busybox:stable
COPY app.sh /app.sh
COPY worker.sh /worker.sh
RUN chmod +x /app.sh /worker.sh
EXPOSE 8080
CMD ["/app.sh"]
