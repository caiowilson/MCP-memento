<?php

declare(strict_types=1);

namespace NorthQuay\Reporting;

use NorthQuay\Entity\Lighthouse;

final class InspectionDigest
{
    /** @return list<string> */
    public function openNoteSummaries(Lighthouse $lighthouse): array
    {
        $summaries = [];

        foreach ($lighthouse->inspectionNotes() as $note) {
            if ($note->isOpen()) {
                $summaries[] = $note->summary();
            }
        }

        return $summaries;
    }
}
