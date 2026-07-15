<?php

declare(strict_types=1);

namespace NorthQuay\Entity;

use Doctrine\Common\Collections\ArrayCollection;
use Doctrine\Common\Collections\Collection;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
final class Lighthouse
{
    /** @var Collection<int, InspectionNote> */
    #[ORM\OneToMany(
        mappedBy: 'lighthouse',
        targetEntity: InspectionNote::class,
        cascade: ['persist'],
        orphanRemoval: true,
    )]
    private Collection $inspectionNotes;

    public function __construct()
    {
        $this->inspectionNotes = new ArrayCollection();
    }

    /** @return Collection<int, InspectionNote> */
    public function inspectionNotes(): Collection
    {
        return $this->inspectionNotes;
    }
}
