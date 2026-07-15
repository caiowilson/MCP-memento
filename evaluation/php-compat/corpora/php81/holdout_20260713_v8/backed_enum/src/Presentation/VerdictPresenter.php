<?php

declare(strict_types=1);

namespace LumenParcel\Presentation;

use LumenParcel\Domain\ParcelVerdict;

final class VerdictPresenter
{
    /** @return array{caption: string, tone: string} */
    public function badge(ParcelVerdict $verdict): array
    {
        return match ($verdict) {
            ParcelVerdict::AwaitingScan => ['caption' => 'Awaiting scan', 'tone' => 'slate'],
            ParcelVerdict::Cleared => ['caption' => 'Cleared', 'tone' => 'green'],
            ParcelVerdict::HeldForReview => ['caption' => 'Held for review', 'tone' => 'amber'],
        };
    }
}
