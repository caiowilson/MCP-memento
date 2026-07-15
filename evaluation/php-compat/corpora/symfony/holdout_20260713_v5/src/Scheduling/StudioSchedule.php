<?php

declare(strict_types=1);

namespace RetrievalHoldout\Studio;

use Doctrine\Common\Collections\Collection;

final class StudioSchedule
{
    public function __construct(private StudioCatalog $studios)
    {
    }

    /** @return Collection<int, SessionSlot> */
    public function slotsFor(int $studioId): Collection
    {
        $studio = $this->studios->get($studioId);

        return $studio->sessionSlots();
    }
}
