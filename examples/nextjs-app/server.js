const http = require("http");

const port = process.env.PORT || 3001;

console.log("Ready in 1.2s");
console.log(`Local: http://localhost:${port}`);

const server = http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/plain" });
  res.end("ok\n");
});

server.listen(port, () => {
  console.log("started server");
});

process.on("SIGTERM", () => {
  server.close(() => process.exit(0));
});
