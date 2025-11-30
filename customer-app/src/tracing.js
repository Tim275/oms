// ⭐ OpenTelemetry Browser Tracing Setup
// Warum brauchen wir das?
// → Automatische Traces für alle fetch() Calls (API Requests)
// → Page Load Performance Tracking
// → User Interaction Tracking (Clicks, etc.)
// → End-to-End Tracing: Browser → Gateway → Orders → Database

import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';
import { Resource } from '@opentelemetry/resources';
import { ATTR_SERVICE_NAME } from '@opentelemetry/semantic-conventions';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-web';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { DocumentLoadInstrumentation } from '@opentelemetry/instrumentation-document-load';
import { UserInteractionInstrumentation } from '@opentelemetry/instrumentation-user-interaction';
import { ZoneContextManager } from '@opentelemetry/context-zone';

// ⭐ OTEL Collector Endpoint - Dynamic Runtime Detection
// → Browser sendet Traces via HTTP (nicht gRPC - Browser können kein gRPC!)
// → Port 4318: OTLP HTTP Endpoint
//
// Best Practice: URL wird zur RUNTIME basierend auf window.location ermittelt!
// → localhost:3000      → http://localhost:4318/v1/traces (port-forward)
// → oms.timourhomelab.org → https://otel.timourhomelab.org/v1/traces (Ingress)
// → *.svc.cluster.local → SKIP (Browser kann K8s DNS nicht auflösen!)
//
// Damit funktioniert DASSELBE Docker Image überall ohne Rebuild!
const getOtelCollectorUrl = () => {
  // 1. Check for explicit env override (build-time, optional)
  const envUrl = process.env.REACT_APP_OTEL_COLLECTOR_URL;

  // 2. Skip Kubernetes internal URLs (browser can't resolve them!)
  if (envUrl && envUrl.includes('.svc.cluster.local')) {
    console.warn('[OTEL] Skipping K8s internal URL, using dynamic detection');
  } else if (envUrl && !envUrl.includes('.svc.cluster.local')) {
    // Explicit non-K8s URL provided - use it
    return envUrl;
  }

  // 3. Dynamic detection based on current browser location
  const hostname = window.location.hostname;
  const protocol = window.location.protocol;

  // Development: localhost → port-forward to OTEL Collector
  if (hostname === 'localhost' || hostname === '127.0.0.1') {
    console.log('[OTEL] Development mode: using localhost:4318');
    return 'http://localhost:4318/v1/traces';
  }

  // Production: *.timourhomelab.org → OTEL Ingress endpoint
  if (hostname.includes('timourhomelab.org')) {
    // Use same protocol (http/https) as the page
    const otelUrl = `${protocol}//otel.timourhomelab.org/v1/traces`;
    console.log('[OTEL] Production mode: using', otelUrl);
    return otelUrl;
  }

  // Fallback: Same origin with /otel-collector path (reverse proxy)
  const fallbackUrl = `${protocol}//${hostname}/otel-collector/v1/traces`;
  console.log('[OTEL] Fallback mode: using', fallbackUrl);
  return fallbackUrl;
};

const OTEL_COLLECTOR_URL = getOtelCollectorUrl();

console.log('Initializing OpenTelemetry Browser Tracing...');
console.log('OTEL Collector URL:', OTEL_COLLECTOR_URL);

// ⭐ Resource: Service Metadata
// → Identifiziert diesen Service in Jaeger
// → service.name = "customer-app" (erscheint in Jaeger Service Liste)
const resource = Resource.default().merge(
  new Resource({
    [ATTR_SERVICE_NAME]: 'customer-app',
    'service.version': '1.0.0',
    'deployment.environment': process.env.NODE_ENV || 'development',
  })
);

// ⭐ TracerProvider: Zentrale Tracing Engine
const provider = new WebTracerProvider({
  resource: resource,
});

// ⭐ OTLP Exporter: Sendet Traces zu OTEL Collector
// → HTTP POST zu http://otel-collector:4318/v1/traces
// → JSON Format (Browser können kein Protobuf)
const exporter = new OTLPTraceExporter({
  url: OTEL_COLLECTOR_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// ⭐ BatchSpanProcessor: Sammelt Spans und sendet in Batches
// → Nicht jede Span einzeln senden (ineffizient!)
// → Sammelt bis zu 512 Spans oder 5 Sekunden
provider.addSpanProcessor(new BatchSpanProcessor(exporter, {
  maxQueueSize: 2048,
  maxExportBatchSize: 512,
  scheduledDelayMillis: 5000,
}));

// ⭐ Context Manager: Verwaltet Trace Context in Browser
// → Nutzt Zone.js für automatische Context Propagation
// → React SyntheticEvents werden korrekt getraced
provider.register({
  contextManager: new ZoneContextManager(),
});

// ⭐ Auto-Instrumentation: Automatisches Tracing ohne Code-Änderungen!
registerInstrumentations({
  instrumentations: [
    // 1️⃣ FetchInstrumentation: Traced alle fetch() API Calls
    // → Automatischer Span für jeden fetch()
    // → Propagiert W3C Trace Context Headers (traceparent, tracestate)
    // → Gateway empfängt trace context → kompletter Trace von Browser bis DB!
    new FetchInstrumentation({
      propagateTraceHeaderCorsUrls: [
        /localhost/,  // Development
        /oms\.timourhomelab\.org/,  // k3d
        /api\.oms\.timourhomelab\.org/,  // Production
      ],
      clearTimingResources: true,
      applyCustomAttributesOnSpan: (span, request, result) => {
        // Custom attributes für bessere Debugging
        if (result instanceof Response) {
          span.setAttribute('http.response.content_length', result.headers.get('content-length') || 0);
        }
      },
    }),

    // 2️⃣ DocumentLoadInstrumentation: Page Load Performance
    // → Misst: DNS, TCP, TLS, Request, Response, DOM Load, Paint
    // → Sichtbar in Jaeger als "documentFetch" und "documentLoad" Spans
    new DocumentLoadInstrumentation(),

    // 3️⃣ UserInteractionInstrumentation: Click Tracking
    // → Erstellt Span bei jedem Button/Link Click
    // → Hilfreich um User Flow zu verstehen
    // → Kann deaktiviert werden wenn zu viele Spans
    new UserInteractionInstrumentation({
      eventNames: ['click', 'submit'],
    }),
  ],
});

console.log('OpenTelemetry Browser Tracing initialized');
console.log('Service: customer-app');
console.log('Traces will be sent to:', OTEL_COLLECTOR_URL);

// Export provider für manuelles Tracing (optional)
export default provider;
