<?php

declare(strict_types=1);

namespace LanternMuseum\Presentation;

use LanternMuseum\Domain\FumigationStage;

final class FumigationBrief
{
    public function caption(FumigationStage $stage): string
    {
        return match ($stage) {
            FumigationStage::SealedForTreatment => 'Treatment chamber sealed',
            FumigationStage::ExposureUnderway => 'Pest exposure underway',
            FumigationStage::ClearedForStorage => 'Cleared for collection storage',
        };
    }
}
