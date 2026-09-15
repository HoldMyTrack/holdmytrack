/**
 * Shown when the viewport centre leaves the basemap extract.
 *
 * The point is to distinguish "this region is not in the demo yet" from "the map
 * is broken", which a blank grey canvas cannot do on its own.
 */
export function CoverageNotice() {
  return (
    <div className="coverage-notice" role="status" data-testid="coverage-notice">
      <strong>Outside the demo basemap.</strong> This preview ships a single Ohio extract, so
      streets and labels stop at the state line. Your tracks would still draw here.
    </div>
  );
}
