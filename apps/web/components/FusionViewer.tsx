// Final fused-answer viewer (Phase 5+).
//
// Renders the merged text with inline [1] [2] citations and a hover-card per
// cite (model + prompt-hash + RAG doc snippet). On disagreement, shows a
// side-by-side "Model A says / Model B says" panel.
export function FusionViewer() {
  return (
    <div
      style={{
        border: "1px dashed var(--cf-border)",
        borderRadius: 12,
        padding: "2rem",
        textAlign: "center",
        color: "var(--cf-muted)",
      }}
    >
      🪪 Fusion viewer + citations (Phase 5)
    </div>
  );
}
