import Link from "next/link";

export default function Home() {
  return (
    <main style={{ maxWidth: 720, margin: "6rem auto", padding: "0 1.5rem", fontFamily: "system-ui, -apple-system, sans-serif" }}>
      <h1 style={{ fontSize: "3rem", margin: 0 }}>
        🧠 CogniFlow
      </h1>
      <p style={{ fontSize: "1.25rem", color: "#666", marginTop: "0.5rem" }}>
        <em>One prompt. The world&apos;s best minds — coordinated.</em>
      </p>

      <section style={{ marginTop: "3rem", padding: "1.5rem", border: "1px solid #e5e5e5", borderRadius: 12, background: "#fafafa" }}>
        <h2 style={{ marginTop: 0 }}>Phase 0 — Skeleton ready</h2>
        <p>The CogniFlow web app is wired and serving. Backend services will come online in Phases 1–7.</p>
        <ul>
          <li>
            <Link href="/playground">→ Open the Playground</Link> <em>(Phase 1+)</em>
          </li>
          <li>
            <Link href="/dashboard">→ View the Eval Dashboard</Link> <em>(Phase 7+)</em>
          </li>
        </ul>
      </section>

      <section style={{ marginTop: "2rem" }}>
        <h3>What this is</h3>
        <p>
          CogniFlow is a self-orchestrating AI routing engine. It decomposes your prompt into a DAG of
          specialized sub-tasks, routes each to the best model (OpenAI, Anthropic, local Ollama, …),
          runs them in parallel, debates conflicting answers, and fuses everything into a citation-rich
          response.
        </p>
      </section>
    </main>
  );
}
