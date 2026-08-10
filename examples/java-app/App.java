import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;

/**
 * A single-file stand-in for a Spring Boot app. It prints the readiness line a
 * Spring Boot application prints on startup, so the runtime's spring-boot
 * profile can detect readiness, without requiring Maven or Spring on disk.
 *
 * Run with: java App.java
 */
public class App {
    public static void main(String[] args) throws IOException {
        System.out.println("Started DemoApplication in 1.2 seconds");
        HttpServer server = HttpServer.create(new InetSocketAddress(8080), 0);
        server.createContext("/", exchange -> {
            byte[] body = "ok\n".getBytes();
            exchange.sendResponseHeaders(200, body.length);
            try (OutputStream os = exchange.getResponseBody()) {
                os.write(body);
            }
        });
        server.start();
    }
}
