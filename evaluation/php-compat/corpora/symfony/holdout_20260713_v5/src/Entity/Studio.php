<?php

declare(strict_types=1);

namespace RetrievalHoldout\Studio;

use Doctrine\Common\Collections\ArrayCollection;
use Doctrine\Common\Collections\Collection;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
final class Studio
{
    /** @var Collection<int, SessionSlot> */
    #[ORM\OneToMany(mappedBy: 'studio', targetEntity: SessionSlot::class)]
    private Collection $sessionSlots;

    public function __construct()
    {
        $this->sessionSlots = new ArrayCollection();
    }

    /** @return Collection<int, SessionSlot> */
    public function sessionSlots(): Collection
    {
        return $this->sessionSlots;
    }
}
