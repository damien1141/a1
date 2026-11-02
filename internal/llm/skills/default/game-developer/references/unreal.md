# Unreal Engine Reference

Production patterns for Unreal Engine 5: C++ Actors/Components, Blueprints, Gameplay Ability System (GAS), Niagara, Lumen, Nanite, and Unreal Insights. Read alongside `engine-architecture.md` for fundamentals.

## Project Structure

```
MyGame/
├── Source/
│   └── MyGame/
│       ├── MyGame.Build.cs          # module deps
│       ├── MyGameCharacter.h/.cpp   # player character
│       ├── MyGameGameMode.h/.cpp    # game mode rules
│       ├── Abilities/               # GAS abilities + effects
│       ├── Actors/                  # gameplay actors
│       ├── Components/              # custom components
│       └── UI/                      # UMG widgets
├── Content/
│   ├── Blueprints/
│   ├── Materials/
│   ├── Meshes/
│   ├── Textures/
│   └── Maps/
├── Config/
└── MyGame.uproject
```

## C++ Class Hierarchy

```
UObjectBase                # base (garbage-collected)
└── UObject                # most things (assets, components)
    ├── AActor             # anything placed in level
    │   ├── APawn          # controllable
    │   │   └── ACharacter # with movement component
    │   ├── AGameMode      # game rules
    │   └── APlayerController
    └── UActorComponent    # behavior on actors
        ├── USceneComponent  # has transform (attachable)
        └── UPrimitiveComponent  # renderable / physical
```

## Actor + Component Pattern

```cpp
// MyCharacter.h
#pragma once

#include "CoreMinimal.h"
#include "GameFramework/Character.h"
#include "MyCharacter.generated.h"

UCLASS()
class MYGAME_API AMyCharacter : public ACharacter
{
    GENERATED_BODY()

public:
    AMyCharacter();

    // Called every frame
    virtual void Tick(float DeltaTime) override;

    // Called to bind input (legacy); new projects use Enhanced Input
    virtual void SetupPlayerInputComponent(class UInputComponent* PlayerInputComponent) override;

    UPROPERTY(VisibleAnywhere, BlueprintReadOnly, Category = "Camera")
    class USpringArmComponent* CameraBoom;

    UPROPERTY(VisibleAnywhere, BlueprintReadOnly, Category = "Camera")
    class UCameraComponent* FollowCamera;

    UPROPERTY(EditDefaultsOnly, BlueprintReadOnly, Category = "Stats")
    float MaxHealth = 100.f;

    UPROPERTY(VisibleAnywhere, BlueprintReadOnly, Category = "Stats")
    float CurrentHealth;

    // Blueprint-implementable event
    UFUNCTION(BlueprintImplementableEvent, Category = "Stats")
    void OnHealthChanged(float NewHealth, float Delta);

protected:
    virtual void BeginPlay() override;

private:
    UFUNCTION()
    void HandleHit(AActor* SelfActor, AActor* OtherActor, FVector NormalImpulse, const FHitResult& Hit);
};
```

### UPROPERTY Specifiers

| Specifier | Use |
|---|---|
| `EditDefaultsOnly` | Edit in CDO only (defaults; per-class) |
| `EditAnywhere` | Edit on every instance |
| `VisibleAnywhere` | Read-only in editor |
| `BlueprintReadOnly` | BP can read but not write |
| `BlueprintReadWrite` | BP can read + write |
| `Category = "X"` | Group in editor UI |
| `meta = (ClampMin = "0.0")` | Numeric clamp |

### UFUNCTION Specifiers

| Specifier | Use |
|---|---|
| `BlueprintCallable` | BP can call this C++ function |
| `BlueprintImplementableEvent` | BP implements; C++ calls |
| `BlueprintNativeEvent` | BP can override; C++ has default impl |
| `Server` / `Client` / `NetMulticast` | Networking (RPC) |
| `Reliable` / `Unreliable` | Network packet guarantee |

## Enhanced Input (Modern)

```cpp
// In character class
UPROPERTY(EditDefaultsOnly, Category = "Input")
class UInputAction* MoveAction;

UPROPERTY(EditDefaultsOnly, Category = "Input")
class UInputMappingContext* DefaultMappingContext;

void AMyCharacter::BeginPlay()
{
    Super::BeginPlay();
    if (APlayerController* PC = Cast<APlayerController>(Controller))
    {
        if (UEnhancedInputLocalPlayerSubsystem* Subsystem =
            ULocalPlayer::GetSubsystem<UEnhancedInputLocalPlayerSubsystem>(PC->GetLocalPlayer()))
        {
            Subsystem->AddMappingContext(DefaultMappingContext, 0);
        }
    }
}

void AMyCharacter::SetupPlayerInputComponent(UInputComponent* PlayerInputComponent)
{
    if (UEnhancedInputComponent* EIC = Cast<UEnhancedInputComponent>(PlayerInputComponent))
    {
        EIC->BindAction(MoveAction, ETriggerEvent::Triggered, this, &AMyCharacter::Move);
    }
}

void AMyCharacter::Move(const FInputActionValue& Value)
{
    FVector2D MoveVector = Value.Get<FVector2D>();
    AddMovementInput(GetActorForwardVector(), MoveVector.Y);
    AddMovementInput(GetActorRightVector(), MoveVector.X);
}
```

## Blueprints vs C++

| Use C++ for | Use Blueprints for |
|---|---|
| Base classes, systems | Concrete variants (specific weapons, specific enemies) |
| Performance-critical logic | Designer-facing parameters |
| Network replication | Visual / audio feedback |
| Math / algorithms | Level-specific events |
| Anything needing inheritance hierarchy | Iterative prototyping |

**Pattern:** C++ base with `BlueprintNativeEvent` hooks → Blueprint subclasses for content. Best of both.

## Gameplay Ability System (GAS)

GAS is Unreal's framework for abilities, attributes, effects. Used for RPGs, action games, anything with stats + skills. Complex but powerful.

### Core Concepts

| Concept | What |
|---|---|
| **Ability** (`UGameplayAbility`) | An action the actor can perform (jump, fire, heal) |
| **Attribute** (`UAttributeSet`) | Numeric stats (Health, Mana, Speed) |
| **Effect** (`UGameplayEffect`) | Modify attributes (damage, buff, debuff) |
| **Tag** (`FGameplayTag`) | Categorical state (Status.Stunned, Cooldown.Fireball) |
| **ASC** (`UAbilitySystemComponent`) | The component that ties it together |

### Ability Example

```cpp
// UMyFireballAbility.h
UCLASS()
class UMyFireballAbility : public UGameplayAbility
{
    GENERATED_BODY()

public:
    UPROPERTY(EditDefaultsOnly, Category = "Fireball")
    TSubclassOf<UGameplayEffect> DamageEffect;

    UPROPERTY(EditDefaultsOnly, Category = "Fireball")
    UAnimMontage* CastMontage;

    virtual void ActivateAbility(const FGameplayAbilitySpecHandle Handle,
                                  const FGameplayAbilityActorInfo* ActorInfo,
                                  const FGameplayAbilityActivationInfo ActivationInfo,
                                  const FGameplayEventData* TriggerEventData) override;

private:
    UFUNCTION()
    void OnMontageCompleted();
};
```

```cpp
// UMyFireballAbility.cpp
void UMyFireballAbility::ActivateAbility(...)
{
    if (!CommitAbility(Handle, ActorInfo, ActivationInfo))
    {
        EndAbility(Handle, ActorInfo, ActivationInfo, true, true);
        return;
    }

    if (UAbilitySystemComponent* ASC = GetAbilitySystemComponentFromActorInfo())
    {
        // Play montage; bind completion
        if (UAnimInstance* AnimInst = ActorInfo->GetAnimInstance())
        {
            const float Duration = AnimInst->Montage_Play(CastMontage);
            FOnMontageEnded EndDelegate;
            EndDelegate.BindUFunction(this, "OnMontageCompleted");
            AnimInst->Montage_SetEndDelegate(EndDelegate, CastMontage);
        }

        // Spawn projectile
        SpawnFireball(ActorInfo->AvatarActor.Get());
    }
}
```

### When to Use GAS

- Multiple character classes with overlapping abilities
- Status effects (stun, poison, buff)
- Cooldowns + resource costs
- Networked abilities (built-in replication)

For simple games (single-player, no abilities), GAS is overkill — use simpler custom systems.

## Niagara (VFX)

Niagara is Unreal's modern VFX system. Replaces the legacy Cascade.

```cpp
// Spawn a Niagara effect
UNiagaraComponent* SpawnEffect(UNiagaraSystem* System, FVector Location)
{
    return UNiagaraFunctionLibrary::SpawnSystemAtLocation(
        GetWorld(), System, Location, FRotator::ZeroRotator,
        FVector(1.f), true, true, ENCPoolMethod::None, true);
}
```

For most use, author Niagara systems in the Niagara editor (visual scripting + HLSL modules).

## Lumen + Nanite (Unreal 5)

**Lumen** — real-time global illumination. Setup:
- Project Settings → Rendering → Global Illumination = Lumen
- Project Settings → Rendering → Reflections = Lumen
- Set up Lightmass importance volume for high-quality GI fallback

**Nanite** — virtualized geometry (millions of triangles). Setup:
- In Static Mesh editor → Nanite → Enable
- Use for hero assets (characters, key environment)
- Don't use for: skeletal meshes (not supported), instanced foliage (use foliage system)

**Caveat:** Lumen + Nanite are GPU-heavy. Mobile / VR / Switch may need to disable them.

## Unreal Insights (Profiling)

**Window → Developer Tools → Insights** or `unrealinsights.exe` for a recorded trace.

Trace channels:
- **CPU** — frame timing, task graph
- **GPU** — render passes
- **Memory** — allocation tracking
- **Asset Loading** — async load times

### Common Bottlenecks

| Bottleneck | Symptom | Fix |
|---|---|---|
| Blueprint hot path | Slow per-frame | Move to C++ |
| Tick on too many actors | High CPU | Disable tick; use timers / event-driven |
| Too many collision queries | High Physics.Simulate | Reduce overlap checks; use LOD on collision |
| Heavy material in render | High GPU | Simplify shader; use LOD material |
| Async load blocking | Hitches | Preload; use Asset Manager |
| Spawning many actors | Frame hitches | Pool actors; use Mass Entity for crowds |

## Mass Entity (Unreal 5 ECS)

For 10k+ entities (crowds, particles, RTS):

```cpp
// Fragment (component)
USTRUCT()
struct FTransformFragment : public FMassFragment
{
    GENERATED_BODY()
    UPROPERTY()
    FVector Location = FVector::ZeroVector;
};

// Processor (system)
UCLASS()
class UMyMovementProcessor : public UMassProcessor
{
    GENERATED_BODY()

    virtual void ConfigureQueries() override
    {
        EntityQuery.AddRequirement<FTransformFragment>(EMassFragmentAccess::ReadWrite);
    }

    virtual void Execute(FMassEntityManager& EntityManager, FMassExecutionContext& Context) override
    {
        EntityQuery.ForEachEntityChunk(EntityManager, Context,
            [](FMassExecutionContext& Context)
            {
                TFloatArrayView Locations = Context.GetMutableVariableDataPtr<FTransformFragment>();
                for (FVector& Loc : Locations)
                {
                    Loc += FVector(0, 0, 1);  // move up
                }
            });
    }
};
```

## Networking (Replication)

```cpp
// In an AActor
UPROPERTY(ReplicatedUsing = OnRep_Health)
float Health = 100.f;

UFUNCTION()
void OnRep_Health(float OldHealth)
{
    // Runs on clients when Health changes
    OnHealthChangedEvent.Broadcast(Health, Health - OldHealth);
}

// Server-only function
UFUNCTION(Server, Reliable)
void Server_TakeDamage(float Amount);
void Server_TakeDamage_Implementation(float Amount)
{
    Health -= Amount;
    if (Health <= 0) Die();
}

// Get lifetime + properties replicated
void GetLifetimeReplicatedProps(TArray<FLifetimeProperty>& OutLifetimeProps) const override
{
    Super::GetLifetimeReplicatedProps(OutLifetimeProps);
    DOREPLIFETIME(AMyCharacter, Health);
}
```

Use **OnlineSubsystem** (Steam, EOS, custom) for production multiplayer. For prototyping, **listen server** is fine.

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Tick on every actor | High CPU | Disable tick by default; enable per-class |
| Blueprint in hot path | Slow | Move to C++ |
| No `UPROPERTY` on UObjects | GC collects live ref | Always tag with `UPROPERTY()` |
| `SpawnActor` in Tick | Hitches | Pool actors |
| ForwardDeclare vs include | Slow compile | Forward declare in headers; include in cpp |
| Heavy `BeginPlay` | Long load | Async load assets |
| Blocking `LoadObject` | Hitches | Use `LoadObjectAsync` / Asset Manager |
| Missing `Super::Tick` | Broken behavior | Always call Super |
| Forgetting `Replicated` flag | Multiplayer desync | Use `UPROPERTY(Replicated)` |
