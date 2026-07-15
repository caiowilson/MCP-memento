<?php
declare(strict_types=1);

namespace VirelliumK9\Application;

use VirelliumK9\Domain\GlintPhase;

final class GlintPhasePresenter
{
    public function label(GlintPhase $phase): string
    {
        return match ($phase) {
            GlintPhase::Seeded => 'Queued',
            GlintPhase::Tempering => 'In progress',
            GlintPhase::Sealed => 'Finished',
        };
    }
}
